package extension

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/klog/v2"
	kubesphererest "kubesphere.io/client-go/rest"
)

type Service interface {
	List(
		context.Context,
		internalextension.ListOptions,
	) (internalextension.ListResult, error)
	Show(
		context.Context,
		string,
		string,
	) (internalextension.ShowResult, error)
	Versions(
		context.Context,
		string,
	) (internalextension.VersionsResult, error)
	Status(
		context.Context,
		string,
	) (internalextension.StatusResult, error)
	Watch(
		context.Context,
		string,
		internalextension.PollOptions,
	) (internalextension.Object[internalextension.InstallPlan], error)
	Install(
		context.Context,
		string,
		internalextension.InstallOptions,
	) (internalextension.Operation, error)
	Upgrade(
		context.Context,
		string,
		internalextension.UpgradeOptions,
	) (internalextension.Operation, error)
	Configure(
		context.Context,
		string,
		internalextension.PlanChanges,
	) (internalextension.Operation, error)
	Uninstall(
		context.Context,
		string,
	) (internalextension.Operation, error)
	Wait(
		context.Context,
		internalextension.Operation,
		internalextension.PollOptions,
	) (internalextension.WaitResult, error)
	Diagnose(
		context.Context,
		string,
		internalextension.DiagnoseOptions,
	) (internalextension.Diagnosis, error)
}

type ServiceFactory func() (Service, error)

func NewCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	command := &cobra.Command{
		Use:   "extension",
		Short: "Discover and manage KubeSphere extensions",
		Args:  cobra.NoArgs,
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			if err := rejectExplicitScope(command); err != nil {
				return err
			}
			return rejectUnsafeVerbosity(command)
		},
	}
	command.AddCommand(
		newListCommand(parent, streams, factory),
		newShowCommand(parent, streams, factory),
		newVersionsCommand(parent, streams, factory),
		newStatusCommand(parent, streams, factory),
		newInstallCommand(parent, streams, factory),
		newUpgradeCommand(parent, streams, factory),
		newConfigureCommand(parent, streams, factory),
		newUninstallCommand(parent, streams, factory),
		newDiagnoseCommand(parent, streams, factory),
	)
	return command
}

func rejectUnsafeVerbosity(command *cobra.Command) error {
	verbosity := int64(0)
	if flag := command.Flag("v"); flag != nil {
		value, err := strconv.ParseInt(flag.Value.String(), 10, 32)
		if err == nil {
			verbosity = value
		}
	}
	if verbosity < 8 && !klog.V(8).Enabled() {
		return nil
	}
	return fmt.Errorf(
		"extension commands require --v=7 or lower because KubeSphere REST debug logging at --v=8 can expose extension configuration",
	)
}

func serviceAfterValidation(factory ServiceFactory) (Service, error) {
	service, err := factory()
	if err != nil {
		return nil, fmt.Errorf("create extension service: %w", err)
	}
	return service, nil
}

func validateCommandPathName(kind, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("invalid %s %q: must be non-empty", kind, name)
	}
	if messages := kubesphererest.IsValidPathSegmentName(name); len(messages) != 0 {
		return fmt.Errorf("invalid %s %q: %v", kind, name, messages)
	}
	return nil
}

func exactExtensionNameArgs(
	command *cobra.Command,
	args []string,
) error {
	if err := cobra.ExactArgs(1)(command, args); err != nil {
		return err
	}
	return validateCommandPathName("extension", args[0])
}

func optionalExtensionNameArgs(
	command *cobra.Command,
	args []string,
) error {
	if err := cobra.MaximumNArgs(1)(command, args); err != nil {
		return err
	}
	if len(args) == 0 {
		return nil
	}
	return validateCommandPathName("extension", args[0])
}
