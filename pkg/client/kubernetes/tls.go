package kubernetes

import (
	"github.com/kubesphere/ksctl/pkg/config"
	"k8s.io/client-go/rest"
)

func toKubernetesTLSClientConfig(cfg config.TLSClientConfig, insecureOverride bool) rest.TLSClientConfig {
	return rest.TLSClientConfig{
		Insecure:   cfg.Insecure || insecureOverride,
		ServerName: cfg.ServerName,
		CertFile:   cfg.CertFile,
		KeyFile:    cfg.KeyFile,
		CAFile:     cfg.CAFile,
		CertData:   []byte(cfg.CertData),
		KeyData:    []byte(cfg.KeyData),
		CAData:     []byte(cfg.CAData),
		NextProtos: cfg.NextProtos,
	}
}
