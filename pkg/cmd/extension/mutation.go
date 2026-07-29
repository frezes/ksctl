package extension

import (
	"fmt"
	"io"
	"strings"
	"time"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type waitFlags struct {
	wait    bool
	timeout time.Duration
}

func (flags *waitFlags) add(command *cobra.Command) {
	command.Flags().BoolVar(
		&flags.wait,
		"wait",
		false,
		"Wait for the extension lifecycle operation to complete",
	)
	command.Flags().DurationVar(
		&flags.timeout,
		"wait-timeout",
		10*time.Minute,
		"Maximum time to wait for lifecycle completion",
	)
}

func (flags waitFlags) validate(command *cobra.Command) error {
	if command.Flags().Changed("wait-timeout") && !flags.wait {
		return fmt.Errorf("--wait-timeout requires --wait")
	}
	if flags.wait && flags.timeout <= 0 {
		return fmt.Errorf("wait timeout must be positive")
	}
	return nil
}

func (flags waitFlags) options(
	action string,
	name string,
	errOut io.Writer,
) *internalextension.PollOptions {
	if !flags.wait {
		return nil
	}
	return &internalextension.PollOptions{
		Timeout: flags.timeout,
		OnState: func(event internalextension.StateEvent) error {
			if _, err := fmt.Fprintf(
				errOut,
				"extension/%s state: %s\n",
				name,
				pendingScalar(event.State),
			); err != nil {
				return fmt.Errorf(
					"write extension %s progress: %w",
					action,
					err,
				)
			}
			return nil
		},
	}
}

func (flags *configurationFlags) addInstall(command *cobra.Command) {
	command.Flags().StringVar(
		&flags.configFile,
		"config",
		"",
		"Read global extension configuration from FILE or -",
	)
	command.Flags().StringSliceVar(
		&flags.clusters,
		"clusters",
		nil,
		"Place the extension on a comma-separated list of clusters",
	)
	command.Flags().StringArrayVar(
		&flags.overrides,
		"override",
		nil,
		"Set a cluster override from CLUSTER=FILE or CLUSTER=-",
	)
}

func (flags *configurationFlags) addChanges(command *cobra.Command) {
	flags.addInstall(command)
	command.Flags().StringArrayVar(
		&flags.removeOverrides,
		"remove-override",
		nil,
		"Remove the named cluster override",
	)
	command.Flags().BoolVar(
		&flags.clearConfig,
		"clear-config",
		false,
		"Remove global extension configuration",
	)
	command.Flags().BoolVar(
		&flags.clearClusterScheduling,
		"clear-cluster-scheduling",
		false,
		"Remove cluster placement and all overrides",
	)
	command.MarkFlagsMutuallyExclusive("config", "clear-config")
	command.MarkFlagsMutuallyExclusive(
		"clusters",
		"clear-cluster-scheduling",
	)
	command.MarkFlagsMutuallyExclusive(
		"override",
		"clear-cluster-scheduling",
	)
	command.MarkFlagsMutuallyExclusive(
		"remove-override",
		"clear-cluster-scheduling",
	)
}

func newInstallCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var version string
	var allClusters bool
	var config configurationFlags
	var wait waitFlags
	command := &cobra.Command{
		Use:     "install NAME",
		Short:   "Install a KubeSphere extension",
		Example: parent + " extension install NAME [--version VERSION]",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := wait.validate(command); err != nil {
				return err
			}
			loaded, err := config.load(command, command.InOrStdin())
			if err != nil {
				return err
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			operation, err := service.Install(
				command.Context(),
				args[0],
				internalextension.InstallOptions{
					Version:     version,
					Config:      loaded.Config,
					Clusters:    loaded.Clusters,
					AllClusters: allClusters,
					Overrides:   loaded.Overrides,
				},
			)
			if err != nil {
				return err
			}
			return finishLifecycle(
				command,
				streams,
				service,
				operation,
				wait.options("install", args[0], streams.ErrOut),
				"install",
				"install",
				"installed",
			)
		},
	}
	command.Flags().StringVar(
		&version,
		"version",
		"",
		"Exact extension version; defaults to status.recommendedVersion",
	)
	config.addInstall(command)
	command.Flags().BoolVar(
		&allClusters,
		"all-clusters",
		false,
		"Install the extension agent on every ready, schedulable Fleet Cluster",
	)
	command.MarkFlagsMutuallyExclusive("clusters", "all-clusters")
	wait.add(command)
	return command
}

func newUpgradeCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var version string
	var config configurationFlags
	var wait waitFlags
	command := &cobra.Command{
		Use:     "upgrade NAME",
		Short:   "Upgrade to an exact KubeSphere extension version",
		Example: parent + " extension upgrade NAME --version VERSION",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(version) == "" {
				return fmt.Errorf("--version requires a non-empty exact version")
			}
			if err := wait.validate(command); err != nil {
				return err
			}
			loaded, err := config.load(command, command.InOrStdin())
			if err != nil {
				return err
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			operation, err := service.Upgrade(
				command.Context(),
				args[0],
				internalextension.UpgradeOptions{
					Version:         version,
					Changes:         config.planChanges(command, loaded),
					RequireWaitable: wait.wait,
				},
			)
			if err != nil {
				return err
			}
			return finishLifecycle(
				command,
				streams,
				service,
				operation,
				wait.options("upgrade", args[0], streams.ErrOut),
				"upgrade",
				"upgrade",
				"upgraded",
			)
		},
	}
	command.Flags().StringVar(
		&version,
		"version",
		"",
		"Exact extension version to install",
	)
	config.addChanges(command)
	wait.add(command)
	return command
}

func newConfigureCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var config configurationFlags
	var wait waitFlags
	command := &cobra.Command{
		Use:     "configure NAME",
		Short:   "Configure an installed KubeSphere extension",
		Example: parent + " extension configure NAME --config FILE",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if !config.hasChanges(command) {
				return fmt.Errorf(
					"configure requires at least one configuration or scheduling change flag",
				)
			}
			if err := wait.validate(command); err != nil {
				return err
			}
			loaded, err := config.load(command, command.InOrStdin())
			if err != nil {
				return err
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			changes := config.planChanges(command, loaded)
			changes.RequireWaitable = wait.wait
			operation, err := service.Configure(
				command.Context(),
				args[0],
				changes,
			)
			if err != nil {
				return err
			}
			return finishLifecycle(
				command,
				streams,
				service,
				operation,
				wait.options("configure", args[0], streams.ErrOut),
				"configure",
				"configuration",
				"configured",
			)
		},
	}
	config.addChanges(command)
	wait.add(command)
	return command
}

func newUninstallCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var wait waitFlags
	command := &cobra.Command{
		Use:     "uninstall NAME",
		Short:   "Uninstall a KubeSphere extension",
		Example: parent + " extension uninstall NAME",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := wait.validate(command); err != nil {
				return err
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			operation, err := service.Uninstall(command.Context(), args[0])
			if err != nil {
				return err
			}
			return finishLifecycle(
				command,
				streams,
				service,
				operation,
				wait.options("uninstall", args[0], streams.ErrOut),
				"uninstall",
				"uninstall",
				"uninstalled",
			)
		},
	}
	wait.add(command)
	return command
}

func finishLifecycle(
	command *cobra.Command,
	streams genericiooptions.IOStreams,
	service Service,
	operation internalextension.Operation,
	waitOptions *internalextension.PollOptions,
	action string,
	requested string,
	completed string,
) error {
	name := operation.Name
	if name == "" {
		name = command.Flags().Arg(0)
	}
	if waitOptions == nil {
		if _, err := fmt.Fprintf(
			streams.Out,
			"extension/%s %s requested\n",
			name,
			requested,
		); err != nil {
			return fmt.Errorf("write extension %s output: %w", action, err)
		}
		return nil
	}
	if _, err := service.Wait(
		command.Context(),
		operation,
		*waitOptions,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		streams.Out,
		"extension/%s %s\n",
		name,
		completed,
	); err != nil {
		return fmt.Errorf("write extension %s output: %w", action, err)
	}
	return nil
}
