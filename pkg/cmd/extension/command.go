package extension

import (
	"context"
	"fmt"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
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
			return rejectExplicitScope(command)
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

func serviceAfterValidation(factory ServiceFactory) (Service, error) {
	service, err := factory()
	if err != nil {
		return nil, fmt.Errorf("create extension service: %w", err)
	}
	return service, nil
}
