package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	authcmd "github.com/kubesphere/ksctl/pkg/cmd/auth"
	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
	kubesphererest "kubesphere.io/client-go/rest"
)

const globalRoleAnnotation = "iam.kubesphere.io/globalrole"

type loginPrompterFactory func(io.Reader, io.Writer) authcmd.Prompter

type whoamiRESTClientGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
	KubeSphereUsername() (string, error)
}

type whoamiUser struct {
	Metadata struct {
		Name        string            `json:"name"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
}

func newAuthCommand(userAgent string, oauth *auth.OAuth, getter whoamiRESTClientGetter) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage KubeSphere authentication",
	}
	cmd.AddCommand(newLoginCommand(userAgent, oauth))
	cmd.AddCommand(newLogoutCommand(userAgent, oauth))
	cmd.AddCommand(newWhoAmICommand(getter))
	return cmd
}

func newWhoAmICommand(getter whoamiRESTClientGetter) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Display the current KubeSphere user and global role",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			user, err := loadWhoAmI(cmd.Context(), getter)
			if err != nil {
				return err
			}
			role := strings.TrimSpace(user.Metadata.Annotations[globalRoleAnnotation])
			if role == "" {
				role = "<none>"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\nGlobal Role: %s\n", user.Metadata.Name, role); err != nil {
				return fmt.Errorf("write whoami output: %w", err)
			}
			return nil
		},
	}
}

func loadWhoAmI(ctx context.Context, getter whoamiRESTClientGetter) (whoamiUser, error) {
	if getter == nil {
		return whoamiUser{}, fmt.Errorf("KubeSphere REST client getter is required")
	}
	username, err := getter.KubeSphereUsername()
	if err != nil {
		return whoamiUser{}, fmt.Errorf("resolve KubeSphere username: %w", err)
	}
	if messages := kubesphererest.IsValidPathSegmentName(username); len(messages) != 0 {
		return whoamiUser{}, fmt.Errorf("invalid username %q: %v", username, messages)
	}
	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		return whoamiUser{}, fmt.Errorf("resolve KubeSphere connection: %w", err)
	}
	client, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(restConfig)
	if err != nil {
		return whoamiUser{}, fmt.Errorf("build KubeSphere client: %w", err)
	}
	raw, err := client.Get().
		AbsPath("/kapis/iam.kubesphere.io/v1beta1/users", username).
		Do(ctx).
		Raw()
	if err != nil {
		return whoamiUser{}, fmt.Errorf("get KubeSphere user %q: %w", username, err)
	}
	var user whoamiUser
	if err := json.Unmarshal(raw, &user); err != nil {
		return whoamiUser{}, fmt.Errorf("decode KubeSphere user %q: %w", username, err)
	}
	if strings.TrimSpace(user.Metadata.Name) == "" {
		return whoamiUser{}, fmt.Errorf("KubeSphere user response is missing metadata.name")
	}
	return user, nil
}

func newLoginCommand(userAgent string, oauth *auth.OAuth) *cobra.Command {
	return newLoginCommandWithPrompter(userAgent, oauth, authcmd.NewTerminalPrompter)
}

func newLoginCommandWithPrompter(userAgent string, oauth *auth.OAuth, newPrompter loginPrompterFactory) *cobra.Command {
	var username string
	var password string
	var fleetName string
	var contextName string

	cmd := &cobra.Command{
		Use:   "login [ENDPOINT]",
		Short: "Log in to KubeSphere",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := ""
			if len(args) == 1 {
				endpoint = args[0]
			}
			resolved, err := authcmd.Resolve(authcmd.ResolveOptions{
				Input: authcmd.Input{
					Endpoint: endpoint,
					Username: username,
					Password: password,
					Fleet:    fleetName,
					Context:  contextName,
				},
				Prompter: newPrompter(cmd.InOrStdin(), cmd.ErrOrStderr()),
			})
			if err != nil {
				return err
			}

			configPath := config.DefaultPath()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			fleet := cfg.Fleets[resolved.Fleet]
			existingEndpoint := strings.TrimRight(strings.TrimSpace(fleet.Host), "/")
			if existingEndpoint != "" && existingEndpoint != resolved.Endpoint {
				return fmt.Errorf(
					"error: fleet %q is already bound to endpoint %q; choose another fleet name",
					resolved.Fleet,
					fleet.Host,
				)
			}

			response, err := oauth.Login(cmd.Context(), auth.TokenRequestOptions{
				Endpoint:  resolved.Endpoint,
				Username:  resolved.Username,
				Password:  resolved.Password,
				UserAgent: userAgent,
				Timeout:   30 * time.Second,
			})
			if err != nil {
				return err
			}

			cfg.CurrentContext = resolved.Context
			fleet.Host = resolved.Endpoint
			if fleet.Users == nil {
				fleet.Users = map[string]config.User{}
			}
			user := fleet.Users[resolved.Username]
			user.Username = resolved.Username
			fleet.Users[resolved.Username] = user
			cfg.Fleets[resolved.Fleet] = fleet
			cfg.Contexts[resolved.Context] = config.Context{Fleet: resolved.Fleet, User: resolved.Username}
			if err := persistLoginState(
				configPath,
				tokencache.DefaultDir(),
				cfg,
				resolved.Fleet,
				resolved.Username,
				tokencache.NewEntry(response, time.Now()),
			); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %q\n", resolved.Context)
			return err
		},
	}
	cmd.Flags().StringVarP(&username, "username", "u", "", "KubeSphere username")
	cmd.Flags().StringVarP(&password, "password", "p", "", "KubeSphere password")
	cmd.Flags().StringVar(&fleetName, "fleet", "", "ksctl fleet name")
	cmd.Flags().StringVar(&contextName, "context", "", "ksctl context name")
	return cmd
}

func persistLoginState(configPath, cacheDir string, cfg *config.Config, fleet, user string, entry tokencache.Entry) error {
	rollback, err := tokencache.SaveWithRollback(cacheDir, fleet, user, entry)
	if err != nil {
		return fmt.Errorf("save token cache: %w", err)
	}
	if err := config.Save(configPath, cfg); err != nil {
		saveErr := fmt.Errorf("save config: %w", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			return errors.Join(saveErr, fmt.Errorf("restore token cache: %w", rollbackErr))
		}
		return saveErr
	}
	return nil
}

func newLogoutCommand(userAgent string, oauth *auth.OAuth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout [CONTEXT]",
		Short: "Log out from KubeSphere",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.DefaultPath())
			if err != nil {
				return err
			}
			contextName := cfg.CurrentContext
			if len(args) == 1 {
				contextName = args[0]
			}
			if contextName == "" {
				return fmt.Errorf("error: context is required")
			}
			ctx, ok := cfg.Contexts[contextName]
			if !ok {
				return fmt.Errorf("error: no context exists with the name: %s", contextName)
			}
			fleet, ok := cfg.Fleets[ctx.Fleet]
			if !ok {
				return fmt.Errorf("error: no fleet exists with the name: %s", ctx.Fleet)
			}
			if _, ok := fleet.Users[ctx.User]; !ok {
				return fmt.Errorf("error: no user exists with the name: %s in fleet: %s", ctx.User, ctx.Fleet)
			}
			entry, loadErr := tokencache.Load(tokencache.DefaultDir(), ctx.Fleet, ctx.User)
			if loadErr == nil && entry.AccessToken != "" && oauth != nil {
				_ = oauth.Logout(cmd.Context(), auth.LogoutOptions{
					Endpoint:        fleet.Host,
					AccessToken:     entry.AccessToken,
					UserAgent:       userAgent,
					Timeout:         30 * time.Second,
					TLSClientConfig: fleet.TLSClientConfig,
				})
			}
			if err := tokencache.Delete(tokencache.DefaultDir(), ctx.Fleet, ctx.User); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Logged out from %q\n", contextName)
			return err
		},
	}
	return cmd
}
