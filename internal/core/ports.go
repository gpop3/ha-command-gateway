package core

// SMSSender est la capacité « envoyer un SMS ». Implémentée par le service SMS.
type SMSSender interface {
	Envoyer(numero, message string) error
}

// Speaker pour le tts
type Speaker interface {
	Parler(cle string, args ...any)
	Bip()
}

// NoopSMS est un SMSSender inerte
type NoopSMS struct{}

func (NoopSMS) Envoyer(numero, message string) error { return ErrServiceIndisponible }

// NoopSpeaker est un Speaker inerte : utilisé quand la voix est désactivée.
type NoopSpeaker struct{}

func (NoopSpeaker) Parler(cle string, args ...any) {}
func (NoopSpeaker) Bip()                           {}

// ErrServiceIndisponible est renvoyée par les no-op qui doivent signaler un échec.
type erreurService string

func (e erreurService) Error() string { return string(e) }

const ErrServiceIndisponible = erreurService("service indisponible")
