package kubesphererest

import (
	"reflect"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
	ksrest "kubesphere.io/client-go/rest"
)

func TestTLSClientConfigMapsEveryField(t *testing.T) {
	source := config.TLSClientConfig{
		Insecure:   true,
		ServerName: "ks.example.com",
		CertFile:   "/tmp/client.crt",
		KeyFile:    "/tmp/client.key",
		CAFile:     "/tmp/ca.crt",
		CertData:   "cert-data",
		KeyData:    "key-data",
		CAData:     "ca-data",
		NextProtos: []string{"h2", "http/1.1"},
	}
	want := ksrest.TLSClientConfig{
		Insecure:   true,
		ServerName: "ks.example.com",
		CertFile:   "/tmp/client.crt",
		KeyFile:    "/tmp/client.key",
		CAFile:     "/tmp/ca.crt",
		CertData:   []byte("cert-data"),
		KeyData:    []byte("key-data"),
		CAData:     []byte("ca-data"),
		NextProtos: []string{"h2", "http/1.1"},
	}

	got := TLSClientConfig(source, false)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TLSClientConfig() = %#v, want %#v", got, want)
	}
	source.NextProtos[0] = "mutated"
	if got.NextProtos[0] != "h2" {
		t.Fatalf("NextProtos aliases source slice: %#v", got.NextProtos)
	}
}

func TestTLSClientConfigAppliesInsecureOverride(t *testing.T) {
	got := TLSClientConfig(config.TLSClientConfig{}, true)
	if !got.Insecure {
		t.Fatal("TLSClientConfig().Insecure = false, want override applied")
	}
}
