package modem

// Client pour modem 4G piloté en commandes AT (SIMCom A7670E Cat-1 HAT,
// compatible SIM7600 et la plupart des modules 3GPP). Les SMS sont
// échangés en mode PDU : accents (UCS2) et SMS longs (concaténés) gérés.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
)

// Config regroupe les paramètres du modem.
type Config struct {
	Port      string // ex: /dev/ttyUSB1 (USB) ou /dev/serial0 (UART du HAT)
	Baud      int    // ignoré en USB, 115200 par défaut sur l'UART
	PIN       string // code PIN de la SIM (vide si désactivé)
	Whitelist string // numéros autorisés, séparés par des virgules
}

// SMS représente un message reçu
type SMS struct {
	Numero  string
	Message string
}

const (
	intervalleLecture = 10 * time.Second
	ageMaxMessage     = 5 * time.Minute // un SMS plus vieux n'est pas exécuté
	delaiConcat       = 3 * time.Minute // délai max d'attente des parties manquantes
	maxTentatives     = 3
)

// Client gère la connexion au modem.
type Client struct {
	cfg       Config
	whitelist []string
	ouvrir    func() (io.ReadWriteCloser, error)

	mu        sync.Mutex // un seul échange AT à la fois
	port      *atPort
	pinRefuse bool
	ref       byte

	parties    map[string]*smsLong
	maintenant func() time.Time

	// Garde-fou anti-écho : un SMS qu'on vient d'envoyer, relu comme reçu
	// (ex. alerte envoyée au numéro de la SIM elle-même), ne doit pas être
	// exécuté comme une commande. Comparaison par préfixe : la relecture peut
	// tronquer les caractères multi-octets (emoji).
	echoMu           sync.Mutex
	recemmentEnvoyes map[string][]smsEnvoye
}

type smsEnvoye struct {
	contenu string
	envoye  time.Time
}

const fenetreEcho = 2 * time.Minute

type smsLong struct {
	numero   string
	total    int
	morceaux map[int]string
	date     time.Time
	vuLe     time.Time
}

// New crée un client et initialise le modem.
func New(cfg Config) (*Client, error) {
	if cfg.Baud == 0 {
		cfg.Baud = 115200
	}
	c := nouveauClient(cfg, func() (io.ReadWriteCloser, error) { return ouvrirSerie(cfg.Port, cfg.Baud) })
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.connecter(); err != nil {
		return nil, fmt.Errorf("%s : %w", i18n.T("erreur.modem.connexion", cfg.Port), err)
	}
	return c, nil
}

func nouveauClient(cfg Config, ouvrir func() (io.ReadWriteCloser, error)) *Client {
	var wl []string
	for _, n := range strings.Split(cfg.Whitelist, ",") {
		if n = normaliserNumero(n); n != "" {
			wl = append(wl, n)
		}
	}
	return &Client{
		cfg:              cfg,
		whitelist:        wl,
		ouvrir:           ouvrir,
		ref:              byte(rand.IntN(256)),
		parties:          map[string]*smsLong{},
		maintenant:       time.Now,
		recemmentEnvoyes: map[string][]smsEnvoye{},
	}
}

// Fermer libère le port série.
func (c *Client) Fermer() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port != nil {
		c.port.fermer()
		c.port = nil
	}
}

// ---- Connexion / initialisation ----

// connecter ouvre le port et prépare le modem. Appelé avec c.mu verrouillé.
func (c *Client) connecter() error {
	if c.port != nil {
		c.port.fermer()
		c.port = nil
	}
	rw, err := c.ouvrir()
	if err != nil {
		return err
	}
	p := nouveauATPort(rw)
	if err := c.initialiser(p); err != nil {
		p.fermer()
		return err
	}
	c.port = p
	return nil
}

func (c *Client) initialiser(p *atPort) error {
	// Synchronisation : le module peut être en train de démarrer.
	var err error
	for range 5 {
		if _, err = p.commande("AT", 2*time.Second); err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("%s : %w", i18n.T("erreur.modem.muet"), err)
	}
	_, _ = p.commande("ATE0", 2*time.Second)      // pas d'écho
	_, _ = p.commande("AT+CMEE=2", 2*time.Second) // erreurs détaillées

	if err := c.deverrouillerSIM(p); err != nil {
		return err
	}

	// Mode PDU ; la SIM peut mettre quelques secondes à être prête pour les SMS.
	for i := range 10 {
		if _, err = p.commande("AT+CMGF=0", 5*time.Second); err == nil {
			break
		}
		if estErreurTransport(err) {
			return err
		}
		if i < 9 {
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		return fmt.Errorf("%s : %w", i18n.T("erreur.modem.sms.init"), err)
	}

	// Notification « +CMTI » à chaque nouveau SMS stocké (lecture immédiate).
	if _, err := p.commande("AT+CNMI=2,1,0,0,0", 5*time.Second); err != nil {
		logx.WarnT("modem.cnmi.refuse", err)
	}

	c.journaliserReseau(p)
	return nil
}

func (c *Client) deverrouillerSIM(p *atPort) error {
	var etat string
	for i := range 10 {
		lignes, err := p.commande("AT+CPIN?", 5*time.Second)
		if err == nil {
			etat = strings.TrimSpace(strings.TrimPrefix(premiere(lignes, "+CPIN:"), "+CPIN:"))
			break
		}
		if estErreurTransport(err) {
			return err
		}
		if i == 9 { // SIM absente, mal insérée…
			return fmt.Errorf("%s : %w", i18n.T("erreur.modem.sim"), err)
		}
		time.Sleep(time.Second)
	}

	switch etat {
	case "READY":
		return nil
	case "SIM PIN":
		if c.cfg.PIN == "" {
			return fmt.Errorf("%s", i18n.T("erreur.modem.pin.requis"))
		}
		if c.pinRefuse { // ne jamais réessayer un PIN refusé : risque de bloquer la SIM
			return fmt.Errorf("%s", i18n.T("erreur.modem.pin.refuse"))
		}
		if _, err := p.commande(fmt.Sprintf("AT+CPIN=%q", c.cfg.PIN), 10*time.Second); err != nil {
			if !estErreurTransport(err) {
				c.pinRefuse = true
			}
			return fmt.Errorf("%s : %w", i18n.T("erreur.modem.pin.refuse"), err)
		}
		logx.InfoT("modem.pin.ok")
		time.Sleep(3 * time.Second) // le module charge la SIM (« SMS DONE »)
		return nil
	default:
		return fmt.Errorf("%s", i18n.T("erreur.modem.sim.etat", etat))
	}
}

func (c *Client) journaliserReseau(p *atPort) {
	signal := "?"
	if l, err := p.commande("AT+CSQ", 3*time.Second); err == nil {
		signal = strings.TrimSpace(strings.TrimPrefix(premiere(l, "+CSQ:"), "+CSQ:"))
	}
	operateur := "?"
	if l, err := p.commande("AT+COPS?", 5*time.Second); err == nil {
		operateur = strings.TrimSpace(strings.TrimPrefix(premiere(l, "+COPS:"), "+COPS:"))
	}
	logx.InfoT("modem.pret", c.cfg.Port, operateur, signal)
}

func premiere(lignes []string, prefixe string) string {
	for _, l := range lignes {
		if strings.HasPrefix(l, prefixe) {
			return l
		}
	}
	return ""
}

// executer lance fn sur le port, en le (ré)ouvrant si nécessaire.
// Une erreur de transport ferme le port : le prochain appel reconnectera.
func (c *Client) executer(fn func(p *atPort) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		if err := c.connecter(); err != nil {
			return err
		}
		logx.InfoT("modem.reconnecte")
	}
	err := fn(c.port)
	if err != nil && estErreurTransport(err) {
		c.port.fermer()
		c.port = nil
	}
	return err
}

// ---- Envoi ----

// EnvoyerSMS envoie un SMS (découpé en plusieurs parties si nécessaire).
func (c *Client) EnvoyerSMS(numero, message string) error {
	c.mu.Lock()
	c.ref++
	ref := c.ref
	c.mu.Unlock()

	// Marqué avant l'envoi : l'écho peut arriver pendant l'attente de confirmation.
	c.marquerEnvoye(numero, message)

	parties, err := encoderSubmit(normaliserNumero(numero), message, ref)
	if err != nil {
		return fmt.Errorf("%s : %w", i18n.T("erreur.modem.encodage"), err)
	}

	for _, partie := range parties {
		var errEnvoi error
		for tentative := range maxTentatives {
			errEnvoi = c.executer(func(p *atPort) error {
				_, err := p.envoyerPDU(partie.Longueur, partie.Hex, 60*time.Second)
				return err
			})
			if errEnvoi == nil {
				break
			}
			if tentative < maxTentatives-1 {
				time.Sleep(time.Duration(1<<tentative) * time.Second)
			}
		}
		if errEnvoi != nil {
			return fmt.Errorf("%s : %w", i18n.T("erreur.modem.sendsms.tentatives", maxTentatives), errEnvoi)
		}
	}
	logx.InfoT("sms.envoye", numero, message)
	return nil
}

// ---- Réception ----

var reCMGL = regexp.MustCompile(`^\+CMGL:\s*(\d+)\s*,\s*(\d+)`)

type entreeSIM struct {
	index int
	etat  int
	pdu   string
}

// EcouterSMS lit les SMS reçus (à chaque notification et toutes les 10 s),
// les supprime de la SIM et envoie ceux des numéros autorisés sur canal.
func (c *Client) EcouterSMS(ctx context.Context, canal chan<- SMS) {
	ticker := time.NewTicker(intervalleLecture)
	defer ticker.Stop()

	for {
		c.relever(ctx, canal)

		var urc <-chan string
		c.mu.Lock()
		if c.port != nil {
			urc = c.port.urc
		}
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-urc:
			time.Sleep(500 * time.Millisecond) // laisse arriver les autres parties
		}
	}
}

func (c *Client) relever(ctx context.Context, canal chan<- SMS) {
	var entrees []entreeSIM
	err := c.executer(func(p *atPort) error {
		lignes, err := p.commande("AT+CMGL=4", 30*time.Second)
		if err != nil {
			return err
		}
		entrees = analyserCMGL(lignes)
		for _, e := range entrees {
			if e.etat > 1 { // brouillons / envoyés : on n'y touche pas
				continue
			}
			if _, err := p.commande(fmt.Sprintf("AT+CMGD=%d", e.index), 10*time.Second); err != nil {
				logx.WarnT("modem.sms.suppression.echouee.smsid", e.index, err)
				if estErreurTransport(err) {
					return err
				}
			} else {
				logx.DebugT("modem.sms.supprime.smsid", e.index)
			}
		}
		return nil
	})
	if err != nil {
		logx.WarnT("modem.sms.erreur.lecture.reconnexion", err)
	}

	for _, e := range entrees {
		if e.etat > 1 {
			continue
		}
		sms, err := decoderDeliver(e.pdu)
		if err != nil {
			if !errors.Is(err, errPasDeliver) {
				logx.WarnT("modem.sms.pdu.invalide", e.index, err)
			}
			continue
		}
		c.recevoir(ctx, sms, canal)
	}
	c.purgerParties(ctx, canal)
}

func analyserCMGL(lignes []string) []entreeSIM {
	var out []entreeSIM
	for i := 0; i < len(lignes); i++ {
		m := reCMGL.FindStringSubmatch(lignes[i])
		if m == nil || i+1 >= len(lignes) {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		etat, _ := strconv.Atoi(m[2])
		i++
		out = append(out, entreeSIM{index: idx, etat: etat, pdu: lignes[i]})
	}
	return out
}

// recevoir gère la concaténation puis livre le message complet.
func (c *Client) recevoir(ctx context.Context, sms *smsRecu, canal chan<- SMS) {
	if sms.Total <= 1 || sms.Ref < 0 {
		c.livrer(ctx, sms.Numero, sms.Texte, sms.Date, canal)
		return
	}
	cle := fmt.Sprintf("%s/%d/%d", sms.Numero, sms.Ref, sms.Total)
	long, ok := c.parties[cle]
	if !ok {
		long = &smsLong{numero: sms.Numero, total: sms.Total, morceaux: map[int]string{}, date: sms.Date, vuLe: c.maintenant()}
		c.parties[cle] = long
	}
	long.morceaux[sms.Sequence] = sms.Texte
	if sms.Date.Before(long.date) {
		long.date = sms.Date
	}
	if len(long.morceaux) >= long.total {
		delete(c.parties, cle)
		c.livrer(ctx, long.numero, long.assembler(), long.date, canal)
	}
}

// purgerParties livre (incomplets) les SMS longs dont des parties n'arrivent jamais.
func (c *Client) purgerParties(ctx context.Context, canal chan<- SMS) {
	for cle, long := range c.parties {
		if c.maintenant().Sub(long.vuLe) > delaiConcat {
			delete(c.parties, cle)
			logx.WarnT("modem.sms.incomplet", long.numero, len(long.morceaux), long.total)
			c.livrer(ctx, long.numero, long.assembler(), long.date, canal)
		}
	}
}

func (l *smsLong) assembler() string {
	seqs := make([]int, 0, len(l.morceaux))
	for s := range l.morceaux {
		seqs = append(seqs, s)
	}
	sort.Ints(seqs)
	var sb strings.Builder
	for _, s := range seqs {
		sb.WriteString(l.morceaux[s])
	}
	return sb.String()
}

func (c *Client) livrer(ctx context.Context, numero, texte string, date time.Time, canal chan<- SMS) {
	if !c.autorise(numero) {
		logx.InfoT("modem.sms.recu.un.numero", numero, texte)
		return
	}
	if strings.TrimSpace(texte) == "" {
		return
	}
	if !date.IsZero() && c.maintenant().Sub(date) > ageMaxMessage {
		logx.InfoT("modem.sms.trop.ancien", numero, texte)
		return
	}
	if c.estEcho(numero, texte) {
		logx.InfoT("modem.sms.echo.ignore", numero, texte)
		return
	}
	logx.InfoT("sms.recu", numero, texte)
	select {
	case canal <- SMS{Numero: numero, Message: strings.ToLower(texte)}:
	case <-ctx.Done():
	}
}

// ---- Anti-écho ----

func cleEcho(numero string) string {
	n := normaliserNumero(numero)
	if len(n) > 9 {
		return n[len(n)-9:]
	}
	return n
}

// marquerEnvoye enregistre un SMS envoyé pour pouvoir reconnaître son écho.
func (c *Client) marquerEnvoye(numero, message string) {
	c.echoMu.Lock()
	defer c.echoMu.Unlock()
	maintenant := c.maintenant()
	cle := cleEcho(numero)
	var gardes []smsEnvoye
	for _, e := range append(c.recemmentEnvoyes[cle], smsEnvoye{contenu: message, envoye: maintenant}) {
		if maintenant.Sub(e.envoye) < fenetreEcho {
			gardes = append(gardes, e)
		}
	}
	c.recemmentEnvoyes[cle] = gardes
}

// estEcho indique si contenu, reçu de numero, est l'écho d'un SMS qu'on lui a envoyé il y a peu.
func (c *Client) estEcho(numero, contenu string) bool {
	c.echoMu.Lock()
	defer c.echoMu.Unlock()
	contenu = strings.TrimSpace(contenu)
	if contenu == "" {
		return false
	}
	maintenant := c.maintenant()
	for _, e := range c.recemmentEnvoyes[cleEcho(numero)] {
		if maintenant.Sub(e.envoye) >= fenetreEcho {
			continue
		}
		envoye := strings.TrimSpace(e.contenu)
		if strings.HasPrefix(envoye, contenu) || strings.HasPrefix(contenu, envoye) {
			return true
		}
	}
	return false
}

// ---- Numéros ----

// normaliserNumero retire espaces/points/tirets et convertit 00 → +.
func normaliserNumero(n string) string {
	n = strings.Map(func(r rune) rune {
		if r == ' ' || r == '.' || r == '-' {
			return -1
		}
		return r
	}, strings.TrimSpace(n))
	if strings.HasPrefix(n, "00") {
		n = "+" + n[2:]
	}
	return n
}

// autorise compare les 9 derniers chiffres pour accepter indifféremment
// 06 12 34 56 78 et +33612345678 dans la whitelist.
func (c *Client) autorise(numero string) bool {
	n := normaliserNumero(numero)
	return slices.ContainsFunc(c.whitelist, func(w string) bool {
		return w == n || (len(w) >= 9 && len(n) >= 9 && w[len(w)-9:] == n[len(n)-9:])
	})
}
