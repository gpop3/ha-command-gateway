package modem

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- PDU ----

func TestDecoderDeliverReference(t *testing.T) {
	// Exemple classique : « hellohello » de +27381000015, GSM 7 bits.
	sms, err := decoderDeliver("07917283010010F5040BC87238880900F10000993092516195800AE8329BFD4697D9EC37")
	if err != nil {
		t.Fatal(err)
	}
	if sms.Texte != "hellohello" {
		t.Fatalf("texte = %q", sms.Texte)
	}
	if sms.Date.Month() != 3 || sms.Date.Day() != 29 {
		t.Fatalf("date = %v", sms.Date)
	}
}

func TestEncoderSubmitSimple(t *testing.T) {
	parties, err := encoderSubmit("+33612345678", "hellohello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(parties) != 1 {
		t.Fatalf("%d parties", len(parties))
	}
	want := "0001000B913316325476F800000AE8329BFD4697D9EC37"
	if parties[0].Hex != want {
		t.Fatalf("PDU = %s, attendu %s", parties[0].Hex, want)
	}
	if parties[0].Longueur != len(want)/2-1 {
		t.Fatalf("longueur = %d", parties[0].Longueur)
	}
}

// submitVersDeliver transforme un SUBMIT encodé en DELIVER décodable (aller-retour).
func submitVersDeliver(t *testing.T, p partiePDU) *smsRecu {
	t.Helper()
	b, _ := hex.DecodeString(p.Hex)
	tpdu := b[1:]
	premier := tpdu[0]&0x40 | 0x04 // DELIVER, UDHI conservé
	lgNum := int(tpdu[2])
	adr := tpdu[2 : 4+(lgNum+1)/2]
	pid, dcs := tpdu[4+(lgNum+1)/2], tpdu[5+(lgNum+1)/2]
	reste := tpdu[6+(lgNum+1)/2:]
	scts := []byte{0x62, 0x11, 0x80, 0x01, 0x00, 0x00, 0x40} // 2026-11-08 10:00:00 +01:00
	d := append([]byte{0x00, premier}, adr...)
	d = append(d, pid, dcs)
	d = append(d, scts...)
	d = append(d, reste...)
	sms, err := decoderDeliver(hex.EncodeToString(d))
	if err != nil {
		t.Fatal(err)
	}
	return sms
}

func TestAllerRetourAccentsEtLong(t *testing.T) {
	cas := []string{
		"Allume la lumière du salon",                   // GSM 7 (è, é existent)
		"Température : 21°C ✓",                         // UCS2
		strings.Repeat("Météo demain : soleil. ", 20),  // GSM 7 long
		strings.Repeat("Chauffage œuvre ° ", 12),       // UCS2 long
		strings.Repeat("{accolades} [crochets] €", 10), // échappements GSM
	}
	for _, msg := range cas {
		parties, err := encoderSubmit("0612345678", msg, 42)
		if err != nil {
			t.Fatal(err)
		}
		var sb strings.Builder
		for i, p := range parties {
			if p.Longueur > 140+13 { // TPDU max = 140 octets de données + en-tête
				t.Fatalf("partie %d trop longue : %d", i, p.Longueur)
			}
			sms := submitVersDeliver(t, p)
			if len(parties) > 1 && (sms.Ref != 42 || sms.Total != len(parties) || sms.Sequence != i+1) {
				t.Fatalf("UDH incorrect : %+v", sms)
			}
			if sms.Numero != "0612345678" {
				t.Fatalf("numéro = %q", sms.Numero)
			}
			sb.WriteString(sms.Texte)
		}
		if sb.String() != msg {
			t.Fatalf("aller-retour :\n got %q\nwant %q", sb.String(), msg)
		}
	}
}

func TestAntiEcho(t *testing.T) {
	c := nouveauClient(Config{Whitelist: "0612345678"}, nil)
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	c.maintenant = func() time.Time { return now }
	c.marquerEnvoye("+33612345678", "🚨 Alerte : porte ouverte")
	if !c.estEcho("0612345678", "🚨 Alerte : porte") {
		t.Fatal("écho tronqué non reconnu")
	}
	if c.estEcho("0612345678", "allume le salon") {
		t.Fatal("vraie commande prise pour un écho")
	}
	now = now.Add(3 * time.Minute)
	if c.estEcho("0612345678", "🚨 Alerte : porte ouverte") {
		t.Fatal("la fenêtre d'écho doit expirer")
	}
}

func TestNumeroAutorise(t *testing.T) {
	c := nouveauClient(Config{Whitelist: "06 12 34 56 78, +33700000000"}, nil)
	for n, ok := range map[string]bool{"+33612345678": true, "0612345678": true, "+33700000000": true, "+33699999999": false, "": false} {
		if c.autorise(n) != ok {
			t.Errorf("autorise(%q) != %v", n, ok)
		}
	}
}

// ---- Modem simulé ----

type faux struct {
	mu       sync.Mutex
	versHote *io.PipeWriter
	hote     *io.PipeReader
	depuis   *io.PipeReader
	ecrit    *io.PipeWriter
	stockes  map[int]string
	envoyes  []string
	pinOK    bool
}

func nouveauFaux() *faux {
	f := &faux{stockes: map[int]string{}}
	f.hote, f.versHote = io.Pipe()
	f.depuis, f.ecrit = io.Pipe()
	go f.boucle()
	return f
}

func (f *faux) Read(b []byte) (int, error)  { return f.hote.Read(b) }
func (f *faux) Write(b []byte) (int, error) { return f.ecrit.Write(b) }
func (f *faux) Close() error {
	_ = f.ecrit.Close()
	return f.versHote.Close()
}

func (f *faux) repondre(s string) { _, _ = io.WriteString(f.versHote, s) }

func (f *faux) boucle() {
	buf := make([]byte, 4096)
	var acc string
	attentePDU := false
	for {
		n, err := f.depuis.Read(buf)
		if err != nil {
			return
		}
		acc += string(buf[:n])
		for {
			if attentePDU {
				i := strings.IndexByte(acc, 0x1a)
				if i < 0 {
					break
				}
				f.mu.Lock()
				f.envoyes = append(f.envoyes, acc[:i])
				f.mu.Unlock()
				acc = acc[i+1:]
				attentePDU = false
				f.repondre("\r\n+CMGS: 12\r\n\r\nOK\r\n")
				continue
			}
			i := strings.IndexByte(acc, '\r')
			if i < 0 {
				break
			}
			cmd := acc[:i]
			acc = acc[i+1:]
			if strings.HasPrefix(cmd, "AT+CMGS=") {
				attentePDU = true
				f.repondre("\r\n> ")
				continue
			}
			f.traiter(cmd)
		}
	}
}

func (f *faux) traiter(cmd string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case cmd == "AT+CPIN?":
		if f.pinOK {
			f.repondre("\r\n+CPIN: READY\r\n\r\nOK\r\n")
		} else {
			f.repondre("\r\n+CPIN: SIM PIN\r\n\r\nOK\r\n")
		}
	case cmd == `AT+CPIN="0000"`:
		f.pinOK = true
		f.repondre("\r\nOK\r\n")
	case strings.HasPrefix(cmd, "AT+CPIN="):
		f.repondre("\r\n+CME ERROR: incorrect password\r\n")
	case cmd == "AT+CSQ":
		f.repondre("\r\n+CSQ: 20,99\r\n\r\nOK\r\n")
	case cmd == "AT+CMGL=4":
		var sb strings.Builder
		for i := 0; i < 50; i++ {
			if pdu, ok := f.stockes[i]; ok {
				sb.WriteString(fmt.Sprintf("\r\n+CMGL: %d,0,,%d\r\n%s", i, len(pdu)/2-1, pdu))
			}
		}
		f.repondre(sb.String() + "\r\n\r\nOK\r\n")
	case strings.HasPrefix(cmd, "AT+CMGD="):
		var i int
		_, _ = fmt.Sscanf(cmd, "AT+CMGD=%d", &i)
		delete(f.stockes, i)
		f.repondre("\r\nOK\r\n")
	default:
		f.repondre("\r\nOK\r\n")
	}
}

func TestClientEnvoiEtReception(t *testing.T) {
	f := nouveauFaux()
	c := nouveauClient(Config{PIN: "0000", Whitelist: "0612345678"}, func() (io.ReadWriteCloser, error) { return f, nil })
	c.maintenant = func() time.Time { return time.Date(2026, 11, 8, 10, 1, 0, 0, time.FixedZone("", 3600)) }
	c.mu.Lock()
	if err := c.connecter(); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	defer c.Fermer()

	long := strings.Repeat("Il fait 21°C dans le salon. ", 6)
	if err := c.EnvoyerSMS("+33612345678", long); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	nb := len(f.envoyes)
	f.mu.Unlock()
	if nb < 2 {
		t.Fatalf("SMS long envoyé en %d partie(s)", nb)
	}

	// Réception d'un SMS long en 2 parties + un SMS d'un inconnu.
	parties, _ := encoderSubmit("+33612345678", "Allume la LUMIÈRE du salon "+strings.Repeat("x", 150), 7)
	f.mu.Lock()
	for i, p := range parties {
		f.stockes[i+1] = deliverDepuis(t, p, "+33612345678")
	}
	inconnu, _ := encoderSubmit("+33699999999", "coucou", 1)
	f.stockes[9] = deliverDepuis(t, inconnu[0], "+33699999999")
	f.mu.Unlock()

	canal := make(chan SMS, 4)
	c.relever(context.Background(), canal)
	select {
	case m := <-canal:
		if !strings.HasPrefix(m.Message, "allume la lumière du salon") || m.Numero != "+33612345678" {
			t.Fatalf("SMS reçu = %+v", m)
		}
	default:
		t.Fatal("aucun SMS livré")
	}
	if len(canal) != 0 {
		t.Fatal("le SMS d'un numéro inconnu ne doit pas être livré")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.stockes) != 0 {
		t.Fatalf("SMS non supprimés de la SIM : %v", f.stockes)
	}
}

func TestPINRefuseNonReessaye(t *testing.T) {
	f := nouveauFaux()
	c := nouveauClient(Config{PIN: "9999"}, func() (io.ReadWriteCloser, error) { return f, nil })
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.connecter(); err == nil {
		t.Fatal("PIN faux accepté")
	}
	if !c.pinRefuse {
		t.Fatal("le refus du PIN doit être mémorisé")
	}
}

func deliverDepuis(t *testing.T, p partiePDU, numero string) string {
	t.Helper()
	b, _ := hex.DecodeString(p.Hex)
	tpdu := b[1:]
	lgNum := int(tpdu[2])
	adr := tpdu[2 : 4+(lgNum+1)/2]
	pid, dcs := tpdu[4+(lgNum+1)/2], tpdu[5+(lgNum+1)/2]
	reste := tpdu[6+(lgNum+1)/2:]
	d := append([]byte{0x00, tpdu[0]&0x40 | 0x04}, adr...)
	d = append(d, pid, dcs, 0x62, 0x11, 0x80, 0x01, 0x00, 0x00, 0x40)
	d = append(d, reste...)
	return strings.ToUpper(hex.EncodeToString(d))
}
