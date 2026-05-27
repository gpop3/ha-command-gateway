package console

import (
	"bufio"
	"fmt"
	"ha-command-gateway/internal/i18n"
	"os"
	"strings"
)

func EcouterConsole() string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println(i18n.T("console.prete"))

	for {
		// La lecture est bloquante, mais seulement dans cette goroutine
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)

		if text != "" {
			return text
		}
	}

}
