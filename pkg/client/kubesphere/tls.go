package kubesphere

import (
	ksrest "kubesphere.io/client-go/rest"
	"kubesphere.io/ksctl/pkg/config"
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
