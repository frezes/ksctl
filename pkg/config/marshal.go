package config

import (
	"encoding/json"

	"sigs.k8s.io/yaml"
)

func Marshal(cfg *Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

func (c Config) MarshalJSON() ([]byte, error) {
	type serializedFleet struct {
		Host            string           `json:"host"`
		TLSClientConfig *TLSClientConfig `json:"tlsClientConfig,omitempty"`
		Users           map[string]User  `json:"users,omitempty"`
	}
	type serializedConfig struct {
		APIVersion     string                     `json:"apiVersion"`
		Kind           string                     `json:"kind"`
		CurrentContext string                     `json:"currentContext,omitempty"`
		Fleets         map[string]serializedFleet `json:"fleets,omitempty"`
		Contexts       map[string]Context         `json:"contexts,omitempty"`
	}

	fleets := make(map[string]serializedFleet, len(c.Fleets))
	for name, fleet := range c.Fleets {
		serialized := serializedFleet{Host: fleet.Host, Users: fleet.Users}
		if !fleet.TLSClientConfig.isZero() {
			tlsConfig := fleet.TLSClientConfig
			serialized.TLSClientConfig = &tlsConfig
		}
		fleets[name] = serialized
	}
	return json.Marshal(serializedConfig{
		APIVersion:     c.APIVersion,
		Kind:           c.Kind,
		CurrentContext: c.CurrentContext,
		Fleets:         fleets,
		Contexts:       c.Contexts,
	})
}

func (c TLSClientConfig) isZero() bool {
	return !c.Insecure && c.ServerName == "" && c.CertFile == "" &&
		c.KeyFile == "" && c.CAFile == "" && c.CertData == "" &&
		c.KeyData == "" && c.CAData == "" && len(c.NextProtos) == 0
}
