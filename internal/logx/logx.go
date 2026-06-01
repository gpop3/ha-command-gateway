package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"ha-command-gateway/internal/i18n"
)

// Niveau de log
type Niveau int32

const (
	NiveauDebug Niveau = iota
	NiveauInfo
	NiveauWarn
	NiveauError
)

var etiquettes = map[Niveau]string{
	NiveauDebug: "DEBUG",
	NiveauInfo:  "INFO ",
	NiveauWarn:  "WARN ",
	NiveauError: "ERROR",
}

var (
	niveauMin atomic.Int32
	sortie    = log.New(os.Stdout, "", log.LstdFlags)
)

func init() {
	SetNiveauDepuisTexte(os.Getenv("LOG_LEVEL"))
}

// SetNiveau fixe le niveau minimal affiché.
func SetNiveau(n Niveau) { niveauMin.Store(int32(n)) }

// SetNiveauDepuisTexte accepte "debug" | "info" | "warn" | "error" (défaut : info).
func SetNiveauDepuisTexte(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		SetNiveau(NiveauDebug)
	case "warn", "warning":
		SetNiveau(NiveauWarn)
	case "error", "erreur":
		SetNiveau(NiveauError)
	default:
		SetNiveau(NiveauInfo)
	}
}

func emettre(n Niveau, msg string) {
	if int32(n) < niveauMin.Load() {
		return
	}
	_ = sortie.Output(2, etiquettes[n]+" "+strings.TrimRight(msg, "\n"))
}

func Debug(args ...any) { emettre(NiveauDebug, fmt.Sprintln(args...)) }
func Info(args ...any)  { emettre(NiveauInfo, fmt.Sprintln(args...)) }
func Warn(args ...any)  { emettre(NiveauWarn, fmt.Sprintln(args...)) }
func Error(args ...any) { emettre(NiveauError, fmt.Sprintln(args...)) }

func Debugf(format string, a ...any) { emettre(NiveauDebug, fmt.Sprintf(format, a...)) }
func Infof(format string, a ...any)  { emettre(NiveauInfo, fmt.Sprintf(format, a...)) }
func Warnf(format string, a ...any)  { emettre(NiveauWarn, fmt.Sprintf(format, a...)) }
func Errorf(format string, a ...any) { emettre(NiveauError, fmt.Sprintf(format, a...)) }

// Fatal / Fatalf logguent en ERROR puis terminent le process.
func Fatal(args ...any)              { emettre(NiveauError, fmt.Sprintln(args...)); os.Exit(1) }
func Fatalf(format string, a ...any) { emettre(NiveauError, fmt.Sprintf(format, a...)); os.Exit(1) }

// ---- API i18n (la clé est résolue dans la locale courante) ----

func DebugT(cle string, a ...any) { emettre(NiveauDebug, i18n.T(cle, a...)) }
func InfoT(cle string, a ...any)  { emettre(NiveauInfo, i18n.T(cle, a...)) }
func WarnT(cle string, a ...any)  { emettre(NiveauWarn, i18n.T(cle, a...)) }
func ErrorT(cle string, a ...any) { emettre(NiveauError, i18n.T(cle, a...)) }
