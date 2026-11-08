package modem

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Erreurs « transport » : le port ne répond plus, il faut le rouvrir.
var (
	errTimeout      = errors.New("timeout AT")
	errPortFerme    = errors.New("port série fermé")
	errPromptAbsent = errors.New("prompt '>' absent")
)

// erreurAT est une réponse ERROR / +CME ERROR / +CMS ERROR du modem :
// le modem fonctionne, c'est la commande qui est refusée.
type erreurAT struct {
	commande string
	reponse  string
}

func (e *erreurAT) Error() string { return fmt.Sprintf("%s → %s", e.commande, e.reponse) }

// estErreurTransport indique si l'erreur impose de rouvrir le port.
func estErreurTransport(err error) bool {
	return errors.Is(err, errTimeout) || errors.Is(err, errPortFerme) || errors.Is(err, errPromptAbsent)
}

// atPort dialogue en commandes AT sur un flux série.
// Une goroutine lit en continu et découpe en lignes ; les notifications
// spontanées « +CMTI: » (nouveau SMS) partent sur urc, le reste sur lignes.
type atPort struct {
	rw     io.ReadWriteCloser
	lignes chan string
	urc    chan string
	fini   chan struct{}
	once   sync.Once
}

func nouveauATPort(rw io.ReadWriteCloser) *atPort {
	p := &atPort{
		rw:     rw,
		lignes: make(chan string, 256),
		urc:    make(chan string, 16),
		fini:   make(chan struct{}),
	}
	go p.lire()
	return p
}

func (p *atPort) fermer() {
	p.once.Do(func() {
		_ = p.rw.Close()
	})
}

func (p *atPort) lire() {
	defer close(p.fini)
	tampon := make([]byte, 0, 1024)
	bloc := make([]byte, 512)
	for {
		n, err := p.rw.Read(bloc)
		if n > 0 {
			tampon = append(tampon, bloc[:n]...)
			tampon = p.decouper(tampon)
		}
		if err != nil {
			return
		}
	}
}

// decouper extrait les lignes complètes et le prompt « > » de CMGS.
func (p *atPort) decouper(tampon []byte) []byte {
	for {
		i := strings.IndexByte(string(tampon), '\n')
		if i < 0 {
			break
		}
		ligne := strings.TrimSpace(string(tampon[:i]))
		tampon = tampon[i+1:]
		if ligne != "" {
			p.emettre(ligne)
		}
	}
	if reste := strings.TrimSpace(string(tampon)); reste == ">" {
		p.emettre(">")
		tampon = tampon[:0]
	}
	return tampon
}

func (p *atPort) emettre(ligne string) {
	if strings.HasPrefix(ligne, "+CMTI:") {
		select {
		case p.urc <- ligne:
		default:
		}
		return
	}
	select {
	case p.lignes <- ligne:
	default: // tampon plein : on jette la plus ancienne
		select {
		case <-p.lignes:
		default:
		}
		select {
		case p.lignes <- ligne:
		default:
		}
	}
}

// vider jette les lignes restées en attente (réponses tardives, URC inconnues).
func (p *atPort) vider() {
	for {
		select {
		case <-p.lignes:
		default:
			return
		}
	}
}

func (p *atPort) ecrire(s string) error {
	select {
	case <-p.fini:
		return errPortFerme
	default:
	}
	if _, err := io.WriteString(p.rw, s); err != nil {
		return fmt.Errorf("%w : %v", errPortFerme, err)
	}
	return nil
}

func estFinale(ligne string) (fin bool, echec bool) {
	switch {
	case ligne == "OK":
		return true, false
	case ligne == "ERROR", ligne == "NO CARRIER",
		strings.HasPrefix(ligne, "+CME ERROR"), strings.HasPrefix(ligne, "+CMS ERROR"):
		return true, true
	}
	return false, false
}

// attendre lit les lignes jusqu'à la réponse finale.
func (p *atPort) attendre(commande string, delai time.Duration) ([]string, error) {
	timer := time.NewTimer(delai)
	defer timer.Stop()
	var reponse []string
	for {
		select {
		case ligne := <-p.lignes:
			if ligne == commande || ligne == ">" { // écho éventuel
				continue
			}
			if fin, echec := estFinale(ligne); fin {
				if echec {
					return reponse, &erreurAT{commande: commande, reponse: ligne}
				}
				return reponse, nil
			}
			reponse = append(reponse, ligne)
		case <-p.fini:
			return reponse, errPortFerme
		case <-timer.C:
			return reponse, fmt.Errorf("%w (%s)", errTimeout, commande)
		}
	}
}

// commande envoie une commande AT et renvoie les lignes de réponse (sans OK).
func (p *atPort) commande(cmd string, delai time.Duration) ([]string, error) {
	p.vider()
	if err := p.ecrire(cmd + "\r"); err != nil {
		return nil, err
	}
	return p.attendre(cmd, delai)
}

// envoyerPDU exécute AT+CMGS=<longueur>, attend le prompt puis envoie le PDU.
func (p *atPort) envoyerPDU(longueur int, pdu string, delai time.Duration) ([]string, error) {
	cmd := fmt.Sprintf("AT+CMGS=%d", longueur)
	p.vider()
	if err := p.ecrire(cmd + "\r"); err != nil {
		return nil, err
	}

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
attente:
	for {
		select {
		case ligne := <-p.lignes:
			if ligne == ">" {
				break attente
			}
			if fin, echec := estFinale(ligne); fin && echec {
				return nil, &erreurAT{commande: cmd, reponse: ligne}
			}
		case <-p.fini:
			return nil, errPortFerme
		case <-timer.C:
			_ = p.ecrire("\x1b") // annule la saisie en cours
			return nil, errPromptAbsent
		}
	}

	if err := p.ecrire(pdu + "\x1a"); err != nil {
		return nil, err
	}
	return p.attendre(cmd, delai)
}
