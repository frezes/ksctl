package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newResourceCommands(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) []*cobra.Command {
	commands := []*cobra.Command{
		getcmd.NewCmdGet(displayName, factory, streams),
		describecmd.NewCmdDescribe(displayName, factory, streams),
		logscmd.NewCmdLogs(factory, streams),
	}
	for _, command := range commands {
		rewriteKubectlExamples(command, displayName)
	}
	return commands
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
