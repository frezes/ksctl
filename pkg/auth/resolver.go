package auth

import (
	"fmt"
	"os"

	"github.com/kubesphere/ksctl/pkg/config"
)

type ResolveInput struct {
	EndpointFlag  string
	TokenFlag     string
	ContextFlag   string
	ClusterFlag   string
	WorkspaceFlag string
	NoInteractive bool
	Env           map[string]string
	Config        *config.Config
}

type Resolved struct {
	Endpoint        string
	Username        string
	ExplicitToken   string
	BearerTokenFile string
	BearerToken     string
	Password        string
	Context         string
	Fleet           string
	User            string
	Cluster         string
	Workspace       string
	TLSClientConfig config.TLSClientConfig
}

func Resolve(in ResolveInput) (Resolved, error) {
	env := in.Env
	if env == nil {
		env = map[string]string{
			"KS_ENDPOINT": os.Getenv("KS_ENDPOINT"),
			"KS_TOKEN":    os.Getenv("KS_TOKEN"),
		}
	}
	cfg := in.Config
	if cfg == nil {
		cfg = config.New()
	}

	out := Resolved{
		Endpoint:      firstNonEmpty(in.EndpointFlag, env["KS_ENDPOINT"]),
		ExplicitToken: firstNonEmpty(in.TokenFlag, env["KS_TOKEN"]),
		Context:       firstNonEmpty(in.ContextFlag, cfg.CurrentContext),
		Cluster:       in.ClusterFlag,
		Workspace:     in.WorkspaceFlag,
	}

	resolveContext := out.Context != "" && (in.ContextFlag != "" || out.Endpoint == "" || out.ExplicitToken == "")
	if resolveContext {
		ctx, ok := cfg.Contexts[out.Context]
		if !ok {
			return out, fmt.Errorf("error: no context exists with the name: %s", out.Context)
		}
		fleet, ok := cfg.Fleets[ctx.Fleet]
		if !ok {
			return out, fmt.Errorf("error: no fleet exists with the name: %s", ctx.Fleet)
		}
		user, ok := fleet.Users[ctx.User]
		if !ok {
			return out, fmt.Errorf("error: no user exists with the name: %s in fleet: %s", ctx.User, ctx.Fleet)
		}
		out.Fleet = ctx.Fleet
		out.User = ctx.User
		if out.Endpoint == "" {
			out.Endpoint = fleet.Host
			out.TLSClientConfig = fleet.TLSClientConfig
		}
		if out.Cluster == "" {
			out.Cluster = ctx.DefaultCluster
		}
		out.Username = user.Username
		if out.Username == "" {
			out.Username = ctx.User
		}
		out.BearerTokenFile = user.BearerTokenFile
		out.BearerToken = user.BearerToken
		out.Password = user.Password
	}

	if out.Endpoint == "" {
		return out, fmt.Errorf("error: KubeSphere endpoint is not configured")
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
