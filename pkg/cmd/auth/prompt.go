package authcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/term"
)

type terminalPrompter struct {
	reader       *bufio.Reader
	out          io.Writer
	interactive  bool
	readPassword func() ([]byte, error)
}

func NewTerminalPrompter(in io.Reader, out io.Writer) Prompter {
	fdReader, ok := in.(interface{ Fd() uintptr })
	interactive := ok && term.IsTerminal(int(fdReader.Fd()))
	prompter := newTerminalPrompter(in, out, interactive)
	if interactive {
		fd := int(fdReader.Fd())
		prompter.readPassword = func() ([]byte, error) {
			return term.ReadPassword(fd)
		}
	}
	return prompter
}

func newTerminalPrompter(in io.Reader, out io.Writer, interactive bool) *terminalPrompter {
	return &terminalPrompter{
		reader:      bufio.NewReader(in),
		out:         out,
		interactive: interactive,
		readPassword: func() ([]byte, error) {
			return nil, errors.New("password input requires a terminal")
		},
	}
}

func (p *terminalPrompter) Available() bool { return p.interactive }

func (p *terminalPrompter) ReadLine(prompt string) (string, error) {
	if _, err := fmt.Fprint(p.out, prompt); err != nil {
		return "", err
	}
	value, err := p.reader.ReadString('\n')
	if err != nil && !(err == io.EOF && len(value) > 0) {
		return "", err
	}
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	return value, nil
}

func (p *terminalPrompter) ReadPassword(prompt string) (string, error) {
	if _, err := fmt.Fprint(p.out, prompt); err != nil {
		return "", err
	}
	value, err := p.readPassword()
	if _, newlineErr := fmt.Fprintln(p.out); err == nil {
		err = newlineErr
	}
	if err != nil {
		return "", err
	}
	return string(value), nil
}
