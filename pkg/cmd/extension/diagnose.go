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
	var verbose bool
	command := &cobra.Command{
		Use:     "diagnose NAME",
		Short:   "Diagnose KubeSphere extension controller state",
		Example: parent + " extension diagnose NAME [--target-cluster CLUSTER] [--verbose]",
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
			if err := printDiagnosis(
				streams.Out,
				args[0],
				diagnosis,
				verbose,
				serviceErr == nil,
			); err != nil {
				return fmt.Errorf(
					"write extension diagnosis output: %w",
					err,
				)
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
	command.Flags().BoolVar(
		&verbose,
		"verbose",
		false,
		"Show every completed diagnostic check",
	)
	return command
}

type diagnosisCounts struct {
	ok    int
	info  int
	warn  int
	error int
}

func countDiagnosis(
	checks []internalextension.DiagnosticCheck,
) diagnosisCounts {
	var counts diagnosisCounts
	for _, check := range checks {
		switch check.Status {
		case internalextension.DiagnosticOK:
			counts.ok++
		case internalextension.DiagnosticInfo:
			counts.info++
		case internalextension.DiagnosticWarn:
			counts.warn++
		case internalextension.DiagnosticError:
			counts.error++
		}
	}
	return counts
}

func printDiagnosis(
	out io.Writer,
	name string,
	diagnosis internalextension.Diagnosis,
	verbose bool,
	complete bool,
) error {
	counts := countDiagnosis(diagnosis.Checks)
	if complete && !verbose && counts.warn == 0 && counts.error == 0 {
		_, err := fmt.Fprintf(
			out,
			"extension/%s: healthy (%d checks passed)\n",
			tableCell(name),
			len(diagnosis.Checks),
		)
		return err
	}

	rows := [][]string{{"CHECK", "STATUS", "MESSAGE"}}
	for _, check := range diagnosis.Checks {
		if !verbose &&
			check.Status != internalextension.DiagnosticWarn &&
			check.Status != internalextension.DiagnosticError {
			continue
		}
		rows = append(rows, []string{
			check.Name,
			string(check.Status),
			check.Message,
		})
	}
	if len(rows) > 1 {
		if err := writeTable(out, rows); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		out,
		"Summary: OK=%d INFO=%d WARN=%d ERROR=%d\n",
		counts.ok,
		counts.info,
		counts.warn,
		counts.error,
	); err != nil {
		return err
	}
	if !complete {
		_, err := fmt.Fprintf(
			out,
			"extension/%s: diagnosis incomplete (%d checks completed)\n",
			tableCell(name),
			len(diagnosis.Checks),
		)
		return err
	}
	return nil
}
