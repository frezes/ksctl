package authcmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestTerminalPrompterWritesPromptsAndReadsLines(t *testing.T) {
	out := new(bytes.Buffer)
	prompter := newTerminalPrompter(strings.NewReader("first\r\nlast"), out, true)

	first, err := prompter.ReadLine("first: ")
	if err != nil || first != "first" {
		t.Fatalf("ReadLine() = %q, %v", first, err)
	}
	last, err := prompter.ReadLine("last: ")
	if err != nil || last != "last" {
		t.Fatalf("ReadLine() final line = %q, %v", last, err)
	}
	if out.String() != "first: last: " {
		t.Fatalf("output = %q", out.String())
	}
}

func TestTerminalPrompterReturnsEOFBeforeValue(t *testing.T) {
	prompter := newTerminalPrompter(strings.NewReader(""), io.Discard, true)
	_, err := prompter.ReadLine("endpoint: ")
	if err != io.EOF {
		t.Fatalf("ReadLine() error = %v, want EOF", err)
	}
}

func TestTerminalPrompterAvailability(t *testing.T) {
	if newTerminalPrompter(strings.NewReader(""), io.Discard, true).Available() != true {
		t.Fatal("interactive Prompter is unavailable")
	}
	if newTerminalPrompter(strings.NewReader(""), io.Discard, false).Available() != false {
		t.Fatal("non-interactive Prompter is available")
	}
	if NewTerminalPrompter(strings.NewReader(""), io.Discard).Available() {
		t.Fatal("plain strings.Reader must not be detected as a terminal")
	}
}
