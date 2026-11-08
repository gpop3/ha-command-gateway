package modem

// Encodage / décodage des SMS en mode PDU (3GPP TS 23.040 / 23.038).
// Le mode PDU permet les accents (UCS2) et les SMS longs (concaténés),
// ce que le mode texte gère mal sur les modules SIMCom.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// ---- Alphabet GSM 7 bits ----

const gsmBase = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\x1bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?" +
	"¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

var (
	gsmVersSeptet = map[rune]byte{}
	gsmDepuis     [128]rune
	gsmExtVers    = map[rune]byte{'\f': 0x0A, '^': 0x14, '{': 0x28, '}': 0x29, '\\': 0x2F, '[': 0x3C, '~': 0x3D, ']': 0x3E, '|': 0x40, '€': 0x65}
	gsmExtDepuis  = map[byte]rune{}
)

func init() {
	i := 0
	for _, r := range gsmBase {
		gsmDepuis[i] = r
		if r != '\x1b' {
			gsmVersSeptet[r] = byte(i)
		}
		i++
	}
	for r, b := range gsmExtVers {
		gsmExtDepuis[b] = r
	}
}

// versGSM7 convertit le texte en septets ; ok=false si un caractère n'existe pas en GSM 7 bits.
// Chaque élément du résultat est un caractère (1 ou 2 septets si extension).
func versGSM7(texte string) (caracteres [][]byte, ok bool) {
	for _, r := range texte {
		if b, ok := gsmVersSeptet[r]; ok {
			caracteres = append(caracteres, []byte{b})
		} else if b, ok := gsmExtVers[r]; ok {
			caracteres = append(caracteres, []byte{0x1b, b})
		} else {
			return nil, false
		}
	}
	return caracteres, true
}

func depuisGSM7(septets []byte) string {
	var sb strings.Builder
	for i := 0; i < len(septets); i++ {
		s := septets[i] & 0x7f
		if s == 0x1b && i+1 < len(septets) {
			i++
			if r, ok := gsmExtDepuis[septets[i]&0x7f]; ok {
				sb.WriteRune(r)
			} else {
				sb.WriteRune(' ')
			}
			continue
		}
		sb.WriteRune(gsmDepuis[s])
	}
	return sb.String()
}

// empaqueter compacte des septets (LSB d'abord) après `decalage` bits de bourrage.
func empaqueter(septets []byte, decalage int) []byte {
	totalBits := decalage + 7*len(septets)
	out := make([]byte, (totalBits+7)/8)
	bit := decalage
	for _, s := range septets {
		for b := 0; b < 7; b++ {
			if s&(1<<b) != 0 {
				out[bit/8] |= 1 << (bit % 8)
			}
			bit++
		}
	}
	return out
}

// depaqueter extrait n septets depuis des octets compactés.
func depaqueter(donnees []byte, n int) []byte {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		var s byte
		for b := 0; b < 7; b++ {
			bit := i*7 + b
			if bit/8 < len(donnees) && donnees[bit/8]&(1<<(bit%8)) != 0 {
				s |= 1 << b
			}
		}
		out = append(out, s)
	}
	return out
}

// ---- Numéros ----

func encoderNumero(numero string) (longueur byte, toa byte, octets []byte, err error) {
	numero = strings.ReplaceAll(numero, " ", "")
	toa = 0x81
	if strings.HasPrefix(numero, "+") {
		toa = 0x91
		numero = numero[1:]
	}
	if numero == "" {
		return 0, 0, nil, errors.New("numéro vide")
	}
	for _, c := range numero {
		if c < '0' || c > '9' {
			return 0, 0, nil, fmt.Errorf("numéro invalide : %q", numero)
		}
	}
	longueur = byte(len(numero))
	if len(numero)%2 == 1 {
		numero += "F"
	}
	for i := 0; i < len(numero); i += 2 {
		octets = append(octets, hexNibble(numero[i+1])<<4|hexNibble(numero[i]))
	}
	return longueur, toa, octets, nil
}

func hexNibble(c byte) byte {
	if c == 'F' {
		return 0xF
	}
	return c - '0'
}

func decoderSemiOctets(octets []byte, chiffres int) string {
	var sb strings.Builder
	for _, o := range octets {
		for _, n := range []byte{o & 0x0F, o >> 4} {
			if sb.Len() >= chiffres || n > 9 {
				break
			}
			sb.WriteByte('0' + n)
		}
	}
	return sb.String()
}

// ---- Encodage SMS-SUBMIT ----

// partiePDU est un PDU prêt pour AT+CMGS.
type partiePDU struct {
	Hex      string // PDU complet (SMSC par défaut "00" inclus)
	Longueur int    // longueur TPDU pour AT+CMGS (sans l'octet SMSC)
}

const (
	maxGSM7Seul   = 160
	maxGSM7Partie = 153 // 160 - 7 septets d'UDH
	maxUCS2Seul   = 70
	maxUCS2Partie = 67 // (140 - 6 octets d'UDH) / 2
)

// encoderSubmit découpe le message en une ou plusieurs parties PDU.
// ref identifie le message concaténé côté destinataire.
func encoderSubmit(numero, message string, ref byte) ([]partiePDU, error) {
	lgNum, toa, num, err := encoderNumero(numero)
	if err != nil {
		return nil, err
	}

	type bloc struct {
		ud  []byte
		udl int
	}
	var blocs []bloc
	dcs := byte(0x00)

	if caracteres, ok := versGSM7(message); ok {
		total := 0
		for _, c := range caracteres {
			total += len(c)
		}
		var parties [][]byte
		if total <= maxGSM7Seul {
			var s []byte
			for _, c := range caracteres {
				s = append(s, c...)
			}
			parties = [][]byte{s}
		} else {
			var courante []byte
			for _, c := range caracteres { // on ne coupe jamais une séquence d'échappement
				if len(courante)+len(c) > maxGSM7Partie {
					parties = append(parties, courante)
					courante = nil
				}
				courante = append(courante, c...)
			}
			parties = append(parties, courante)
		}
		for i, s := range parties {
			if len(parties) == 1 {
				blocs = append(blocs, bloc{ud: empaqueter(s, 0), udl: len(s)})
				continue
			}
			udh := []byte{0x05, 0x00, 0x03, ref, byte(len(parties)), byte(i + 1)}
			// UDH = 48 bits + 1 bit de bourrage → les septets démarrent sur une frontière de septet
			ud := append(udh, empaqueter(s, 1)...)
			blocs = append(blocs, bloc{ud: ud, udl: 7 + len(s)})
		}
	} else {
		dcs = 0x08
		unites := utf16.Encode([]rune(message))
		var parties [][]uint16
		if len(unites) <= maxUCS2Seul {
			parties = [][]uint16{unites}
		} else {
			for len(unites) > 0 {
				n := min(maxUCS2Partie, len(unites))
				if n < len(unites) && utf16.IsSurrogate(rune(unites[n-1])) && unites[n-1] < 0xDC00 {
					n-- // ne pas couper une paire de substitution
				}
				parties = append(parties, unites[:n])
				unites = unites[n:]
			}
		}
		for i, u := range parties {
			var ud []byte
			if len(parties) > 1 {
				ud = []byte{0x05, 0x00, 0x03, ref, byte(len(parties)), byte(i + 1)}
			}
			for _, x := range u {
				ud = append(ud, byte(x>>8), byte(x))
			}
			blocs = append(blocs, bloc{ud: ud, udl: len(ud)})
		}
	}

	if len(blocs) > 255 {
		return nil, errors.New("message trop long")
	}

	var out []partiePDU
	for _, b := range blocs {
		premier := byte(0x01) // SMS-SUBMIT, pas de période de validité
		if len(blocs) > 1 {
			premier |= 0x40 // UDHI
		}
		tpdu := []byte{premier, 0x00, lgNum, toa}
		tpdu = append(tpdu, num...)
		tpdu = append(tpdu, 0x00, dcs, byte(b.udl))
		tpdu = append(tpdu, b.ud...)
		out = append(out, partiePDU{
			Hex:      strings.ToUpper("00" + hex.EncodeToString(tpdu)),
			Longueur: len(tpdu),
		})
	}
	return out, nil
}

// ---- Décodage SMS-DELIVER ----

// smsRecu est un SMS (ou une partie de SMS concaténé) décodé.
type smsRecu struct {
	Numero   string
	Texte    string
	Date     time.Time
	Ref      int // référence de concaténation (-1 si SMS simple)
	Total    int
	Sequence int
}

var errPasDeliver = errors.New("PDU qui n'est pas un SMS-DELIVER")

func decoderDeliver(pduHex string) (*smsRecu, error) {
	pdu, err := hex.DecodeString(strings.TrimSpace(pduHex))
	if err != nil {
		return nil, err
	}
	l := lecteur{donnees: pdu}

	lgSMSC := int(l.octet())
	l.sauter(lgSMSC)

	premier := l.octet()
	if premier&0x03 != 0x00 {
		return nil, errPasDeliver
	}
	udhi := premier&0x40 != 0

	lgNum := int(l.octet())
	toa := l.octet()
	octetsNum := l.lire((lgNum + 1) / 2)
	var numero string
	if toa&0x70 == 0x50 { // alphanumérique
		numero = depuisGSM7(depaqueter(octetsNum, lgNum*4/7))
	} else {
		numero = decoderSemiOctets(octetsNum, lgNum)
		if toa&0x70 == 0x10 {
			numero = "+" + numero
		}
	}

	l.octet() // PID
	dcs := l.octet()
	date := decoderSCTS(l.lire(7))
	udl := int(l.octet())
	ud := l.reste()
	if l.err != nil {
		return nil, l.err
	}

	sms := &smsRecu{Numero: numero, Date: date, Ref: -1, Total: 1, Sequence: 1}

	lgUDH := 0
	if udhi && len(ud) > 0 {
		lgUDH = int(ud[0]) + 1
		if lgUDH > len(ud) {
			return nil, errors.New("UDH invalide")
		}
		lireUDH(ud[1:lgUDH], sms)
	}

	switch alphabet(dcs) {
	case 2: // UCS2
		corps := ud[lgUDH:]
		if len(corps)%2 == 1 {
			corps = corps[:len(corps)-1]
		}
		unites := make([]uint16, len(corps)/2)
		for i := range unites {
			unites[i] = uint16(corps[2*i])<<8 | uint16(corps[2*i+1])
		}
		sms.Texte = string(utf16.Decode(unites))
	case 1: // 8 bits
		sms.Texte = string(ud[lgUDH:])
	default: // GSM 7 bits
		septets := depaqueter(ud, udl)
		saut := (lgUDH*8 + 6) / 7
		if saut > len(septets) {
			saut = len(septets)
		}
		sms.Texte = depuisGSM7(septets[saut:])
	}
	return sms, nil
}

// alphabet : 0 = GSM 7 bits, 1 = 8 bits, 2 = UCS2.
func alphabet(dcs byte) int {
	switch {
	case dcs&0xC0 == 0x00:
		if v := int(dcs>>2) & 0x03; v != 3 {
			return v
		}
		return 0
	case dcs&0xF0 == 0xF0:
		return int(dcs>>2) & 0x01
	case dcs&0xF0 == 0xE0:
		return 2
	}
	return 0
}

func lireUDH(udh []byte, sms *smsRecu) {
	for i := 0; i+1 < len(udh); {
		iei, lg := udh[i], int(udh[i+1])
		val := udh[i+2:]
		if lg > len(val) {
			return
		}
		val = val[:lg]
		switch {
		case iei == 0x00 && lg == 3:
			sms.Ref, sms.Total, sms.Sequence = int(val[0]), int(val[1]), int(val[2])
		case iei == 0x08 && lg == 4:
			sms.Ref, sms.Total, sms.Sequence = int(val[0])<<8|int(val[1]), int(val[2]), int(val[3])
		}
		i += 2 + lg
	}
}

func decoderSCTS(o []byte) time.Time {
	if len(o) < 7 {
		return time.Time{}
	}
	bcd := func(b byte) int { return int(b&0x0F)*10 + int(b>>4) }
	quarts := int(o[6]&0x07)*10 + int(o[6]>>4)
	if o[6]&0x08 != 0 {
		quarts = -quarts
	}
	zone := time.FixedZone("", quarts*15*60)
	return time.Date(2000+bcd(o[0]), time.Month(bcd(o[1])), bcd(o[2]), bcd(o[3]), bcd(o[4]), bcd(o[5]), 0, zone)
}

type lecteur struct {
	donnees []byte
	pos     int
	err     error
}

func (l *lecteur) lire(n int) []byte {
	if l.err != nil || n < 0 || l.pos+n > len(l.donnees) {
		l.err = errors.New("PDU tronqué")
		return make([]byte, max(n, 0))
	}
	b := l.donnees[l.pos : l.pos+n]
	l.pos += n
	return b
}

func (l *lecteur) octet() byte  { return l.lire(1)[0] }
func (l *lecteur) sauter(n int) { l.lire(n) }
func (l *lecteur) reste() []byte {
	if l.err != nil {
		return nil
	}
	return l.donnees[l.pos:]
}
