//go:build !linux

package modem

import (
	"fmt"
	"io"

	"ha-command-gateway/internal/i18n"
)

// ouvrirSerie n'est implémenté que sous Linux (Raspberry Pi).
func ouvrirSerie(port string, baud int) (io.ReadWriteCloser, error) {
	return nil, fmt.Errorf("%s", i18n.T("sms.indispo.windows"))
}
