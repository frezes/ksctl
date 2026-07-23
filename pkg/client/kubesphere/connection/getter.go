package connection

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	internalrest "github.com/kubesphere/ksctl/internal/kubesphererest"
	"github.com/kubesphere/ksctl/pkg/auth"
	clientoptions "github.com/kubesphere/ksctl/pkg/client"
	"github.com/kubesphere/ksctl/pkg/config"
	kubesphererest "kubesphere.io/client-go/rest"
)

type Dependencies struct {
	TokenProvider auth.TokenProvider
	Transport     http.RoundTripper
}

type RESTClientGetter struct {
	options       *clientoptions.Options
	tokenProvider auth.TokenProvider
	transport     http.RoundTripper

	configOnce      sync.Once
	restConfig      *kubesphererest.Config
	resolvedCluster string
	configErr       error

	identityOnce sync.Once
	username     string
	identityErr  error
}

func NewRESTClientGetter(options *clientoptions.Options, dependencies Dependencies) *RESTClientGetter {
	if options == nil {
		options = &clientoptions.Options{}
	}
	provider := dependencies.TokenProvider
	if provider == nil {
		provider = auth.NewProvider(auth.ProviderOptions{})
	}
	return &RESTClientGetter{
		options:       options,
		tokenProvider: provider,
		transport:     dependencies.Transport,
	}
}

func (g *RESTClientGetter) ToRESTConfig() (*kubesphererest.Config, error) {
	g.loadConfig()
	if g.configErr != nil {
		return nil, g.configErr
	}
	return kubesphererest.CopyConfig(g.restConfig), nil
}

func (g *RESTClientGetter) KubeSphereCluster() (string, error) {
	g.loadConfig()
	return g.resolvedCluster, g.configErr
}

func (g *RESTClientGetter) KubeSphereUsername() (string, error) {
	g.identityOnce.Do(func() {
		cfg, err := config.Load(g.configPath())
		if err != nil {
			g.identityErr = err
			return
		}
		contextName := g.options.Context
		if contextName == "" {
			contextName = cfg.CurrentContext
		}
		if contextName == "" {
			g.identityErr = fmt.Errorf("error: current-context is not set")
			return
		}
		selected, ok := cfg.Contexts[contextName]
		if !ok {
			g.identityErr = fmt.Errorf("error: no context exists with the name: %s", contextName)
			return
		}
		fleet, ok := cfg.Fleets[selected.Fleet]
		if !ok {
			g.identityErr = fmt.Errorf("error: no fleet exists with the name: %s", selected.Fleet)
			return
		}
		user, ok := fleet.Users[selected.User]
		if !ok {
			g.identityErr = fmt.Errorf("error: no user exists with the name: %s in fleet: %s", selected.User, selected.Fleet)
			return
		}
		g.username = user.Username
		if g.username == "" {
			g.username = selected.User
		}
	})
	return g.username, g.identityErr
}

func (g *RESTClientGetter) loadConfig() {
	g.configOnce.Do(func() {
		cfg, err := config.Load(g.configPath())
		if err != nil {
			g.configErr = err
			return
		}
		resolved, err := auth.Resolve(auth.ResolveInput{
			EndpointFlag: g.options.Endpoint,
			TokenFlag:    g.options.Token,
			ContextFlag:  g.options.Context,
			ClusterFlag:  g.options.Cluster,
			Config:       cfg,
		})
		if err != nil {
			g.configErr = err
			return
		}
		g.resolvedCluster = resolved.Cluster
		if resolved.Cluster != "" {
			if messages := kubesphererest.IsValidPathSegmentName(resolved.Cluster); len(messages) != 0 {
				g.configErr = fmt.Errorf("invalid cluster %q: %v", resolved.Cluster, messages)
				return
			}
		}

		timeout, err := parseKubeSphereTimeout(g.options.RequestTimeout)
		if err != nil {
			g.configErr = err
			return
		}
		resolvedToken, err := g.tokenProvider.Token(context.Background(), resolved, auth.TokenOptions{
			UserAgent:             g.options.UserAgent,
			Timeout:               timeout,
			InsecureSkipTLSVerify: g.options.InsecureSkipTLSVerify,
			TLSClientConfig:       resolved.TLSClientConfig,
		})
		if err != nil {
			g.configErr = err
			return
		}

		g.restConfig = &kubesphererest.Config{
			Host:        resolved.Endpoint,
			BearerToken: resolvedToken,
			UserAgent:   g.options.UserAgent,
			Timeout:     timeout,
		}
		if g.transport != nil {
			g.restConfig.Transport = g.transport
		} else {
			g.restConfig.TLSClientConfig = internalrest.TLSClientConfig(resolved.TLSClientConfig, g.options.InsecureSkipTLSVerify)
		}
	})
}

func (g *RESTClientGetter) configPath() string {
	if g.options.ConfigPath != "" {
		return g.options.ConfigPath
	}
	return config.DefaultPath()
}

func parseKubeSphereTimeout(value string) (time.Duration, error) {
	if value == "" || value == "0" {
		return 0, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid request timeout %q: %w", value, err)
	}
	return timeout, nil
}
