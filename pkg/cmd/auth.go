package cmd

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
)

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
	var username string
	var password string
	var contextName string

	cmd := &cobra.Command{
		Use:   "login ENDPOINT",
		Short: "Log in to KubeSphere",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := strings.TrimRight(args[0], "/")
			if username == "" {
				return fmt.Errorf("error: --username is required")
			}
			if password == "" {
				return fmt.Errorf("error: --password is required")
			}
			if contextName == "" {
				contextName = defaultLoginContextName(endpoint)
			}

			response, err := oauth.Login(cmd.Context(), auth.TokenRequestOptions{
				Endpoint:  endpoint,
				Username:  username,
				Password:  password,
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
			cfg.CurrentContext = contextName
			cfg.Clusters[contextName] = config.Cluster{Host: endpoint}
			cfg.Users[username] = config.User{Username: username}
			cfg.Contexts[contextName] = config.Context{Cluster: contextName, User: username}
			if err := config.Save(config.DefaultPath(), cfg); err != nil {
				return err
			}
			if err := tokencache.Save(tokencache.DefaultDir(), contextName, tokencache.NewEntry(response, time.Now())); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %q\n", contextName)
			return err
		},
	}
	cmd.Flags().StringVarP(&username, "username", "u", "", "KubeSphere username")
	cmd.Flags().StringVarP(&password, "password", "p", "", "KubeSphere password")
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
			if err := tokencache.Delete(tokencache.DefaultDir(), contextName); err != nil {
				return err
			}
			if ctx, ok := cfg.Contexts[contextName]; ok {
				if user, ok := cfg.Users[ctx.User]; ok {
					user.BearerToken = ""
					user.BearerTokenFile = ""
					cfg.Users[ctx.User] = user
				}
			}
			if err := config.Save(config.DefaultPath(), cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Logged out from %q\n", contextName)
			return err
		},
	}
	return cmd
}

func defaultLoginContextName(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err == nil && parsed.Host != "" {
		return tokencache.SafeName(parsed.Host)
	}
	return tokencache.SafeName(endpoint)
}
