package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func Visible(question string) (string, error) {
	fmt.Fprint(os.Stdout, question)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Hidden reads a password without echoing. Requires a TTY.
func Hidden(question string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("hidden prompt requires a TTY")
	}
	fmt.Fprint(os.Stdout, question)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
