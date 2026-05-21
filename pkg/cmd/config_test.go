package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
)

func TestConfigCurrentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "current-context"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(out.String()) != "prod" {
		t.Fatalf("current context = %q", out.String())
	}
}

func TestConfigUseContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "use-context", "prod"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentContext != "prod" {
		t.Fatalf("current context = %q", loaded.CurrentContext)
	}
}

func TestConfigView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "view"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "currentContext: prod") {
		t.Fatalf("config view output = %s", out.String())
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
