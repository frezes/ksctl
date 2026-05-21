package cmd

import (
	"fmt"

	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func newConfigCommand(streams IOStreams) *cobra.Command {
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

	cmd.AddCommand(&cobra.Command{
		Use:   "view",
		Short: "Display merged ksctl config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.DefaultPath())
			if err != nil {
				return err
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	})

	return cmd
}
