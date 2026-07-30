package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootGetAndKubeGetProduceEquivalentResults(t *testing.T) {
	server := newClusterScopedCoreAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	var outputs []string
	for _, prefix := range [][]string{{"get"}, {"kube", "get"}} {
		out := new(bytes.Buffer)
		command := NewRootCommand(
			IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
			VersionInfo{Version: "test"},
		)
		args := append([]string{}, prefix...)
		args = append(args,
			"pods",
			"--all-namespaces",
			"--endpoint", server.URL,
			"--token", "secret",
			"--cluster", "member",
			"-o", "json",
		)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != outputs[1] {
		t.Fatalf("root get output differs from kube get:\nroot:\n%s\nkube:\n%s", outputs[0], outputs[1])
	}
}

func TestKubeRequestTimeoutLimitsRawGet(t *testing.T) {
	const helperEnv = "KSCTL_TEST_KUBE_REQUEST_TIMEOUT"
	if os.Getenv(helperEnv) == "1" {
		command := NewRootCommand(
			IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
			VersionInfo{Version: "test"},
		)
		command.SetArgs([]string{
			"kube", "get",
			"--raw=/slow",
			"--request-timeout=20ms",
			"--endpoint", os.Getenv("KSCTL_TEST_KUBE_REQUEST_TIMEOUT_ENDPOINT"),
			"--token", "secret",
		})
		_ = command.Execute()
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	start := time.Now()
	helper := exec.Command(os.Args[0], "-test.run=^TestKubeRequestTimeoutLimitsRawGet$")
	helper.Env = append(
		os.Environ(),
		helperEnv+"=1",
		"KSCTL_TEST_KUBE_REQUEST_TIMEOUT_ENDPOINT="+server.URL,
		"KSCTL_CONFIG="+filepath.Join(t.TempDir(), "config.yaml"),
	)
	output, err := helper.CombinedOutput()
	if err == nil {
		t.Fatalf("timeout helper succeeded, output:\n%s", output)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request timeout took %s, want less than one second", elapsed)
	}
	if !strings.Contains(strings.ToLower(string(output)), "timeout") &&
		!strings.Contains(strings.ToLower(string(output)), "deadline exceeded") {
		t.Fatalf("timeout helper output = %q, want timeout error", output)
	}
}
