package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	clientoptions "github.com/kubesphere/ksctl/pkg/client"
	"github.com/kubesphere/ksctl/pkg/config"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const clientConfigName = "ksctl"

type Options = clientoptions.Options

type Dependencies struct {
	TokenProvider auth.TokenProvider
	Transport     http.RoundTripper
}

type RESTClientGetter struct {
	options       *Options
	tokenProvider auth.TokenProvider
	transport     http.RoundTripper

	configOnce            sync.Once
	baseConfig            *rest.Config
	coreV1DiscoveryConfig *rest.Config
	clientConfig          clientcmd.ClientConfig
	resolvedCluster       string
	configErr             error

	discoveryOnce   sync.Once
	discoveryClient discovery.CachedDiscoveryInterface
	discoveryErr    error

	mapperOnce sync.Once
	mapper     meta.RESTMapper
	mapperErr  error
}

func NewRESTClientGetter(options *Options, dependencies Dependencies) *RESTClientGetter {
	if options == nil {
		options = &Options{}
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

func (g *RESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	g.loadConfig()
	if g.configErr != nil {
		return nil, g.configErr
	}
	return rest.CopyConfig(g.baseConfig), nil
}

func (g *RESTClientGetter) KubeSphereCluster() (string, error) {
	g.loadConfig()
	return g.resolvedCluster, g.configErr
}

func (g *RESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	g.discoveryOnce.Do(func() {
		cfg, err := g.ToRESTConfig()
		if err != nil {
			g.discoveryErr = err
			return
		}
		client, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			g.discoveryErr = err
			return
		}
		var coreV1Fallback discovery.DiscoveryInterface
		if g.coreV1DiscoveryConfig != nil {
			coreV1Fallback, err = discovery.NewDiscoveryClientForConfig(g.coreV1DiscoveryConfig)
			if err != nil {
				g.discoveryErr = err
				return
			}
		}
		g.discoveryClient = memory.NewMemCacheClient(newFallbackDiscoveryClient(client, coreV1Fallback))
	})
	return g.discoveryClient, g.discoveryErr
}

func (g *RESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	g.mapperOnce.Do(func() {
		client, err := g.ToDiscoveryClient()
		if err != nil {
			g.mapperErr = err
			return
		}
		mapper := restmapper.NewDeferredDiscoveryRESTMapper(client)
		g.mapper = restmapper.NewShortcutExpander(mapper, client, nil)
	})
	return g.mapper, g.mapperErr
}

func (g *RESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return deferredClientConfig{getter: g}
}

func (g *RESTClientGetter) loadConfig() {
	g.configOnce.Do(func() {
		path := g.options.ConfigPath
		if path == "" {
			path = config.DefaultPath()
		}
		cfg, err := config.Load(path)
		if err != nil {
			g.configErr = err
			return
		}
		resolved, err := auth.Resolve(auth.ResolveInput{
			EndpointFlag:  g.options.Endpoint,
			TokenFlag:     g.options.Token,
			ContextFlag:   g.options.Context,
			ClusterFlag:   g.options.Cluster,
			NoInteractive: g.options.NoInteractive,
			Config:        cfg,
		})
		if err != nil {
			g.configErr = err
			return
		}
		g.resolvedCluster = resolved.Cluster
		if resolved.Cluster != "" {
			if messages := rest.IsValidPathSegmentName(resolved.Cluster); len(messages) != 0 {
				g.configErr = fmt.Errorf("invalid cluster %q: %v", resolved.Cluster, messages)
				return
			}
		}

		timeout, err := parseTimeout(g.options.RequestTimeout)
		if err != nil {
			g.configErr = err
			return
		}
		tlsConfig := toKubernetesTLSClientConfig(resolved.TLSClientConfig, g.options.InsecureSkipTLSVerify)
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
		resourceEndpoint := resolved.Endpoint
		if resolved.Cluster != "" {
			resourceEndpoint, err = url.JoinPath(resolved.Endpoint, "clusters", resolved.Cluster)
			if err != nil {
				g.configErr = fmt.Errorf("build endpoint for cluster %q: %w", resolved.Cluster, err)
				return
			}
		}
		g.baseConfig = &rest.Config{
			Host:        resourceEndpoint,
			BearerToken: resolvedToken,
			UserAgent:   g.options.UserAgent,
			Timeout:     timeout,
		}
		if g.transport == nil {
			g.baseConfig.TLSClientConfig = tlsConfig
		} else {
			g.baseConfig.Transport = g.transport
		}
		if resolved.Cluster != "" {
			g.coreV1DiscoveryConfig = rest.CopyConfig(g.baseConfig)
			g.coreV1DiscoveryConfig.Host = resolved.Endpoint
		}

		raw := clientcmdapi.NewConfig()
		raw.Clusters[clientConfigName] = &clientcmdapi.Cluster{
			Server:                   resourceEndpoint,
			InsecureSkipTLSVerify:    tlsConfig.Insecure,
			TLSServerName:            tlsConfig.ServerName,
			CertificateAuthority:     tlsConfig.CAFile,
			CertificateAuthorityData: tlsConfig.CAData,
		}
		raw.AuthInfos[clientConfigName] = &clientcmdapi.AuthInfo{Token: resolvedToken}
		raw.Contexts[clientConfigName] = &clientcmdapi.Context{
			Cluster:  clientConfigName,
			AuthInfo: clientConfigName,
		}
		raw.CurrentContext = clientConfigName

		overrides := &clientcmd.ConfigOverrides{}
		overrides.Context.Namespace = g.options.Namespace
		g.clientConfig = clientcmd.NewNonInteractiveClientConfig(
			*raw,
			clientConfigName,
			overrides,
			nil,
		)
	})
}

func parseTimeout(value string) (time.Duration, error) {
	if value == "" || value == "0" {
		return 0, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid request timeout %q: %w", value, err)
	}
	return timeout, nil
}

type deferredClientConfig struct {
	getter *RESTClientGetter
}

func (c deferredClientConfig) RawConfig() (clientcmdapi.Config, error) {
	clientConfig, err := c.resolved()
	if err != nil {
		return clientcmdapi.Config{}, err
	}
	return clientConfig.RawConfig()
}

func (c deferredClientConfig) ClientConfig() (*rest.Config, error) {
	return c.getter.ToRESTConfig()
}

func (c deferredClientConfig) Namespace() (string, bool, error) {
	clientConfig, err := c.resolved()
	if err != nil {
		return "", false, err
	}
	return clientConfig.Namespace()
}

func (c deferredClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}

func (c deferredClientConfig) resolved() (clientcmd.ClientConfig, error) {
	c.getter.loadConfig()
	return c.getter.clientConfig, c.getter.configErr
}
