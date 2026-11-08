//go:build linux

package modem

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"ha-command-gateway/internal/i18n"
)

var vitesses = map[int]uint32{
	9600:   syscall.B9600,
	19200:  syscall.B19200,
	38400:  syscall.B38400,
	57600:  syscall.B57600,
	115200: syscall.B115200,
	230400: syscall.B230400,
	460800: syscall.B460800,
	921600: syscall.B921600,
}

// ouvrirSerie ouvre le port série en mode brut (8N1, sans contrôle de flux).
// Le descripteur est non bloquant et géré par le poller Go : Close débloque
// une lecture en cours.
func ouvrirSerie(port string, baud int) (io.ReadWriteCloser, error) {
	vitesse, ok := vitesses[baud]
	if !ok {
		return nil, fmt.Errorf("%s", i18n.T("erreur.modem.baud", baud))
	}

	f, err := os.OpenFile(port, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}

	t := syscall.Termios{
		Iflag:  syscall.IGNPAR,
		Cflag:  vitesse | syscall.CS8 | syscall.CREAD | syscall.CLOCAL,
		Ispeed: vitesse,
		Ospeed: vitesse,
	}
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0

	raw, err := f.SyscallConn()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	var errno syscall.Errno
	if err := raw.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&t)))
	}); err != nil {
		_ = f.Close()
		return nil, err
	}
	if errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%s : %w", i18n.T("erreur.modem.termios"), errno)
	}
	return f, nil
}
