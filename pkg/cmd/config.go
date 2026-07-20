package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
	kubesphererest "kubesphere.io/client-go/rest"
)

type kubeconfigRESTClientGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
	KubeSphereCluster() (string, error)
	KubeSphereUsername() (string, error)
}

func newConfigCommand(getter kubeconfigRESTClientGetter) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage ksctl contexts",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "current-context",
		Short: "Display the current context",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.DefaultPath())
			if err != nil {
				return err
			}
			if cfg.CurrentContext == "" {
				return fmt.Errorf("error: current-context is not set")
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), cfg.CurrentContext)
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "use-context NAME",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.DefaultPath())
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Contexts[name]; !ok {
				return fmt.Errorf("error: no context exists with the name: %s", name)
			}
			cfg.CurrentContext = name
			if err := config.Save(config.DefaultPath(), cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q.\n", name)
			return err
		},
	})

	var raw bool
	view := &cobra.Command{
		Use:   "view",
		Short: "Display merged ksctl config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.DefaultPath())
			if err != nil {
				return err
			}
			if !raw {
				cfg = config.RedactedCopy(cfg)
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	view.Flags().BoolVar(&raw, "raw", false, "Display raw config values, including credentials")
	cmd.AddCommand(view)

	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate client configuration",
	}
	generate.AddCommand(&cobra.Command{
		Use:   "kubeconfig",
		Short: "Generate kubeconfig for the current logged-in user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := generateKubeconfig(cmd.Context(), getter)
			if err != nil {
				return err
			}
			if _, err := io.Copy(cmd.OutOrStdout(), bytes.NewReader(data)); err != nil {
				return fmt.Errorf("write kubeconfig: %w", err)
			}
			return nil
		},
	})
	cmd.AddCommand(generate)

	return cmd
}

func generateKubeconfig(ctx context.Context, getter kubeconfigRESTClientGetter) ([]byte, error) {
	username, err := getter.KubeSphereUsername()
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig username: %w", err)
	}
	if messages := kubesphererest.IsValidPathSegmentName(username); len(messages) != 0 {
		return nil, fmt.Errorf("invalid username %q: %v", username, messages)
	}

	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig connection: %w", err)
	}
	cluster, err := getter.KubeSphereCluster()
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig cluster: %w", err)
	}
	client, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build KubeSphere client: %w", err)
	}

	request := client.Get()
	if cluster != "" {
		request.Cluster(cluster)
	}
	raw, err := request.AbsPath("/kapis/resources.kubesphere.io/v1alpha2/users", username, "kubeconfig").
		Do(ctx).
		Raw()
	if err != nil {
		return nil, fmt.Errorf("get kubeconfig for user %q: %w", username, err)
	}
	return raw, nil
}
