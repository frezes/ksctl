package extension

import (
	"fmt"
	"io"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kubesphererest "kubesphere.io/client-go/rest"
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
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if targetCluster != "" {
				if messages := kubesphererest.IsValidPathSegmentName(
					targetCluster,
				); len(messages) != 0 {
					return fmt.Errorf(
						"invalid target cluster %q: %v",
						targetCluster,
						messages,
					)
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
