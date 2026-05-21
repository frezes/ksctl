package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootVersion(t *testing.T) {
	streams := IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}
	cmd := NewRootCommand(streams, VersionInfo{
		Version:   "v0.1.0",
		Commit:    "abc123",
		BuildDate: "2026-05-21",
		GoVersion: "go1.26.0",
	})
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := streams.Out.(*bytes.Buffer).String()
	for _, want := range []string{"Client Version: v0.1.0", "Git Commit: abc123", "Build Date: 2026-05-21"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output missing %q in:\n%s", want, got)
		}
	}
}

func TestRootHelpUsesCommandName(t *testing.T) {
	streams := IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}
	cmd := NewRootCommand(streams, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(streams.Out.(*bytes.Buffer).String(), "ksctl") {
		t.Fatalf("help output should mention ksctl")
	}
}

func TestRootRegistersNativeResourceCommands(t *testing.T) {
	cmd := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})

	getCommand := findSubcommand(cmd, "get")
	if getCommand == nil {
		t.Fatal("get command is not registered")
	}
	if findSubcommand(cmd, "describe") == nil {
		t.Fatal("describe command is not registered")
	}
	if findSubcommand(cmd, "list") != nil {
		t.Fatal("list command is registered")
	}
	for _, name := range []string{"output", "watch", "watch-only", "selector"} {
		if getCommand.Flags().Lookup(name) == nil {
			t.Errorf("get flag --%s is not registered", name)
		}
	}
	if describeCommand := findSubcommand(cmd, "describe"); describeCommand != nil {
		if describeCommand.Flags().Lookup("show-events") == nil {
			t.Error("describe flag --show-events is not registered")
		}
	}
}

func TestRootRegistersNestedAuthCommands(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	auth := findSubcommand(root, "auth")
	if auth == nil {
		t.Fatal("auth command is not registered")
	}
	for _, name := range []string{"login", "logout"} {
		if findSubcommand(auth, name) == nil {
			t.Fatalf("auth %s command is not registered", name)
		}
		if findSubcommand(root, name) != nil {
			t.Fatalf("top-level %s command is registered", name)
		}
	}
}

func TestRootConnectionFlags(t *testing.T) {
	cmd := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	for _, name := range []string{
		"endpoint",
		"token",
		"context",
		"cluster",
		"workspace",
		"namespace",
		"request-timeout",
		"insecure-skip-tls-verify",
		"no-interactive",
		"v",
	} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("persistent flag --%s is not registered", name)
		}
	}
}

func TestRootAcceptsVerbosityFlag(t *testing.T) {
	streams := IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}
	cmd := NewRootCommand(streams, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"-v=8", "version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(streams.Out.(*bytes.Buffer).String(), "Client Version: dev") {
		t.Fatalf("version output = %q", streams.Out.(*bytes.Buffer).String())
	}
}

func findSubcommand(root *cobra.Command, name string) *cobra.Command {
	for _, command := range root.Commands() {
		if command.Name() == name {
			return command
		}
	}
	return nil
}
