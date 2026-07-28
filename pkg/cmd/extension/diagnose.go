package extension

import (
	"fmt"
	"io"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func newDiagnoseCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var targetCluster string
	command := &cobra.Command{
		Use:     "diagnose NAME",
		Short:   "Diagnose KubeSphere extension controller state",
		Example: parent + " extension diagnose NAME [--target-cluster CLUSTER]",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if command.Flags().Changed("target-cluster") {
				if err := validateCommandPathName(
					"target cluster",
					targetCluster,
				); err != nil {
					return err
				}
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			diagnosis, serviceErr := service.Diagnose(
				command.Context(),
				args[0],
				internalextension.DiagnoseOptions{
					TargetCluster: targetCluster,
				},
			)
			if len(diagnosis.Checks) != 0 {
				if err := printDiagnosis(streams.Out, diagnosis); err != nil {
					return fmt.Errorf(
						"write extension diagnosis output: %w",
						err,
					)
				}
			}
			if serviceErr != nil {
				return serviceErr
			}
			return diagnosis.Err()
		},
	}
	command.Flags().StringVar(
		&targetCluster,
		"target-cluster",
		"",
		"Inspect one member cluster status and its host executor workload",
	)
	return command
}

func printDiagnosis(
	out io.Writer,
	diagnosis internalextension.Diagnosis,
) error {
	rows := make([][]string, 0, len(diagnosis.Checks)+1)
	rows = append(rows, []string{"CHECK", "STATUS", "MESSAGE"})
	for _, check := range diagnosis.Checks {
		rows = append(rows, []string{
			check.Name,
			string(check.Status),
			check.Message,
		})
	}
	return writeTable(out, rows)
}
