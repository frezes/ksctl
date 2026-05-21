package config

const (
	ConfigAPIVersion = "ksctl.kubesphere.io/v1alpha1"
	ConfigKind       = "Config"
)

type Config struct {
	APIVersion     string             `json:"apiVersion" yaml:"apiVersion"`
	Kind           string             `json:"kind" yaml:"kind"`
	CurrentContext string             `json:"currentContext,omitempty" yaml:"currentContext,omitempty"`
	Clusters       map[string]Cluster `json:"clusters,omitempty" yaml:"clusters,omitempty"`
	Users          map[string]User    `json:"users,omitempty" yaml:"users,omitempty"`
	Contexts       map[string]Context `json:"contexts,omitempty" yaml:"contexts,omitempty"`
}

type Cluster struct {
	Host            string          `json:"host" yaml:"host"`
	TLSClientConfig TLSClientConfig `json:"tlsClientConfig,omitempty" yaml:"tlsClientConfig,omitempty"`
}

type TLSClientConfig struct {
	Insecure   bool     `json:"insecure,omitempty" yaml:"insecure,omitempty"`
	ServerName string   `json:"serverName,omitempty" yaml:"serverName,omitempty"`
	CertFile   string   `json:"certFile,omitempty" yaml:"certFile,omitempty"`
	KeyFile    string   `json:"keyFile,omitempty" yaml:"keyFile,omitempty"`
	CAFile     string   `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	CertData   string   `json:"certData,omitempty" yaml:"certData,omitempty"`
	KeyData    string   `json:"keyData,omitempty" yaml:"keyData,omitempty"`
	CAData     string   `json:"caData,omitempty" yaml:"caData,omitempty"`
	NextProtos []string `json:"nextProtos,omitempty" yaml:"nextProtos,omitempty"`
}

type User struct {
	Username        string `json:"username,omitempty" yaml:"username,omitempty"`
	BearerToken     string `json:"bearerToken,omitempty" yaml:"bearerToken,omitempty"`
	BearerTokenFile string `json:"bearerTokenFile,omitempty" yaml:"bearerTokenFile,omitempty"`
}

type Context struct {
	Cluster          string `json:"cluster" yaml:"cluster"`
	User             string `json:"user" yaml:"user"`
	DefaultCluster   string `json:"defaultCluster,omitempty" yaml:"defaultCluster,omitempty"`
	DefaultWorkspace string `json:"defaultWorkspace,omitempty" yaml:"defaultWorkspace,omitempty"`
}

func New() *Config {
	return &Config{
		APIVersion: ConfigAPIVersion,
		Kind:       ConfigKind,
		Clusters:   map[string]Cluster{},
		Users:      map[string]User{},
		Contexts:   map[string]Context{},
	}
}
