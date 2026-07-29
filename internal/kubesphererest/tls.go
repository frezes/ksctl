package kubesphererest

import (
	"github.com/kubesphere/ksctl/pkg/config"
	ksrest "kubesphere.io/client-go/rest"
)

func TLSClientConfig(cfg config.TLSClientConfig, insecureOverride bool) ksrest.TLSClientConfig {
	return ksrest.TLSClientConfig{
		Insecure:   cfg.Insecure || insecureOverride,
		ServerName: cfg.ServerName,
		CertFile:   cfg.CertFile,
		KeyFile:    cfg.KeyFile,
		CAFile:     cfg.CAFile,
		CertData:   []byte(cfg.CertData),
		KeyData:    []byte(cfg.KeyData),
		CAData:     []byte(cfg.CAData),
		NextProtos: append([]string(nil), cfg.NextProtos...),
	}
}
