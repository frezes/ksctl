package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	authcmd "github.com/kubesphere/ksctl/pkg/cmd/auth"
	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
)

type loginPrompterFactory func(io.Reader, io.Writer) authcmd.Prompter

func newAuthCommand(userAgent string, oauth *auth.OAuth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage KubeSphere authentication",
	}
	cmd.AddCommand(newLoginCommand(userAgent, oauth))
	cmd.AddCommand(newLogoutCommand())
	return cmd
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

			cfg, err := config.Load(config.DefaultPath())
			if err != nil {
				return err
			}
			cfg.CurrentContext = resolved.Context
			fleet := cfg.Fleets[resolved.Fleet]
			fleet.Host = resolved.Endpoint
			if fleet.Users == nil {
				fleet.Users = map[string]config.User{}
			}
			user := fleet.Users[resolved.Username]
			user.Username = resolved.Username
			fleet.Users[resolved.Username] = user
			cfg.Fleets[resolved.Fleet] = fleet
			cfg.Contexts[resolved.Context] = config.Context{Fleet: resolved.Fleet, User: resolved.Username}
			if err := config.Save(config.DefaultPath(), cfg); err != nil {
				return err
			}
			if err := tokencache.Save(tokencache.DefaultDir(), resolved.Fleet, resolved.Username, tokencache.NewEntry(response, time.Now())); err != nil {
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

func newLogoutCommand() *cobra.Command {
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
			if err := tokencache.Delete(tokencache.DefaultDir(), ctx.Fleet, ctx.User); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Logged out from %q\n", contextName)
			return err
		},
	}
	return cmd
}
