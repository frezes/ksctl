package authcmd

import (
	"fmt"
	"net/url"
	"strings"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
)

type Input struct {
	Endpoint string
	Username string
	Password string
	Fleet    string
	Context  string
}

type Prompter interface {
	Available() bool
	ReadLine(string) (string, error)
	ReadPassword(string) (string, error)
}

type ResolveOptions struct {
	Input    Input
	Prompter Prompter
}

func Resolve(options ResolveOptions) (Input, error) {
	resolved := options.Input
	resolved.Endpoint = normalizeEndpoint(resolved.Endpoint)
	resolved.Username = strings.TrimSpace(resolved.Username)
	resolved.Fleet = strings.TrimSpace(resolved.Fleet)
	resolved.Context = strings.TrimSpace(resolved.Context)
	guided := resolved.Endpoint == "" || resolved.Username == "" || resolved.Password == ""
	interactive := guided && options.Prompter != nil && options.Prompter.Available()

	if resolved.Endpoint == "" {
		value, err := readRequired(options.Prompter, interactive, "endpoint", "endpoint: ", "error: endpoint is required")
		if err != nil {
			return Input{}, err
		}
		resolved.Endpoint = normalizeEndpoint(value)
		if resolved.Endpoint == "" {
			return Input{}, fmt.Errorf("error: endpoint is required")
		}
	}
	if resolved.Username == "" {
		value, err := readRequired(options.Prompter, interactive, "username", "username: ", "error: --username is required")
		if err != nil {
			return Input{}, err
		}
		resolved.Username = strings.TrimSpace(value)
		if resolved.Username == "" {
			return Input{}, fmt.Errorf("error: --username is required")
		}
	}
	if resolved.Password == "" {
		if !interactive {
			return Input{}, fmt.Errorf("error: --password is required")
		}
		value, err := options.Prompter.ReadPassword("password: ")
		if err != nil {
			return Input{}, fmt.Errorf("error: read password: %w", err)
		}
		resolved.Password = value
		if resolved.Password == "" {
			return Input{}, fmt.Errorf("error: --password is required")
		}
	}

	defaultFleet := DefaultFleetName(resolved.Endpoint)
	if resolved.Fleet == "" {
		if interactive {
			value, err := options.Prompter.ReadLine(fmt.Sprintf("fleet [%s]: ", defaultFleet))
			if err != nil {
				return Input{}, fmt.Errorf("error: read fleet: %w", err)
			}
			resolved.Fleet = strings.TrimSpace(value)
		}
		if resolved.Fleet == "" {
			resolved.Fleet = defaultFleet
		}
	}

	defaultContext := DefaultContextName(resolved.Fleet, resolved.Username)
	if resolved.Context == "" {
		if interactive {
			value, err := options.Prompter.ReadLine(fmt.Sprintf("context [%s]: ", defaultContext))
			if err != nil {
				return Input{}, fmt.Errorf("error: read context: %w", err)
			}
			resolved.Context = strings.TrimSpace(value)
		}
		if resolved.Context == "" {
			resolved.Context = defaultContext
		}
	}
	return resolved, nil
}

func readRequired(prompter Prompter, interactive bool, field, prompt, required string) (string, error) {
	if !interactive {
		return "", fmt.Errorf("%s", required)
	}
	value, err := prompter.ReadLine(prompt)
	if err != nil {
		return "", fmt.Errorf("error: read %s: %w", field, err)
	}
	return value, nil
}

func DefaultFleetName(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err == nil && parsed.Host != "" {
		return tokencache.SafeName(parsed.Host)
	}
	return tokencache.SafeName(endpoint)
}

func DefaultContextName(fleet, username string) string {
	return tokencache.SafeName(fleet + "-" + username)
}

func normalizeEndpoint(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}
