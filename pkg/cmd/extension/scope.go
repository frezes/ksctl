package extension

import (
	"fmt"

	"github.com/spf13/cobra"
)

func rejectExplicitScope(command *cobra.Command) error {
	root := command.Root()
	if flag := root.PersistentFlags().Lookup("cluster"); flag != nil && flag.Changed {
		return fmt.Errorf(
			"--cluster is not supported by extension commands; use --clusters for placement or diagnose --target-cluster to select member status",
		)
	}
	if flag := root.PersistentFlags().Lookup("namespace"); flag != nil && flag.Changed {
		return fmt.Errorf(
			"--namespace is not supported because extension resources are cluster-scoped",
		)
	}
	return nil
}
