package client

type Options struct {
	Endpoint              string
	Token                 string
	Context               string
	Cluster               string
	Namespace             string
	RequestTimeout        string
	InsecureSkipTLSVerify bool
	ConfigPath            string
	UserAgent             string
}
