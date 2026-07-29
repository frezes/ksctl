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
	return nil
}
