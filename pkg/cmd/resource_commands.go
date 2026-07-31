package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newRootGetCommand(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) *cobra.Command {
	command := getcmd.NewCmdGet(displayName, factory, streams)
	addNamespaceFlag(command, namespace)
	rewriteKubectlExamples(command, displayName)
	return command
}

func addNamespaceFlag(command *cobra.Command, namespace *string) {
	command.Flags().StringVarP(
		namespace,
		"namespace",
		"n",
		"",
		"Kubernetes namespace or KubeSphere project",
	)
}

func rewriteKubectlExamples(command *cobra.Command, displayName string) {
	command.Example = strings.ReplaceAll(
		command.Example,
		"kubectl ",
		displayName+" ",
	)
	for _, child := range command.Commands() {
		rewriteKubectlExamples(child, displayName)
	}
}
