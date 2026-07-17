package authcmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"golang.org/x/term"
)

type terminalPrompter struct {
	reader      *bufio.Reader
	out         io.Writer
	interactive bool
}

func NewTerminalPrompter(in io.Reader, out io.Writer) Prompter {
	fdReader, ok := in.(interface{ Fd() uintptr })
	interactive := ok && term.IsTerminal(int(fdReader.Fd()))
	return newTerminalPrompter(in, out, interactive)
}

func newTerminalPrompter(in io.Reader, out io.Writer, interactive bool) *terminalPrompter {
	return &terminalPrompter{reader: bufio.NewReader(in), out: out, interactive: interactive}
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
