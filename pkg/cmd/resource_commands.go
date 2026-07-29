package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	topcmd "k8s.io/kubectl/pkg/cmd/top"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newResourceCommands(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) []*cobra.Command {
	get := getcmd.NewCmdGet(displayName, factory, streams)
	describe := describecmd.NewCmdDescribe(displayName, factory, streams)
	logs := logscmd.NewCmdLogs(factory, streams)
	for _, command := range []*cobra.Command{get, describe, logs} {
		addNamespaceFlag(command, namespace)
	}

	commands := []*cobra.Command{
		get,
		describe,
		logs,
		newTopCommand(factory, streams, namespace),
	}
	for _, command := range commands {
		rewriteKubectlExamples(command, displayName)
	}
	return commands
}

func newTopCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) *cobra.Command {
	command := topcmd.NewCmdTop(factory, streams)
	for _, child := range command.Commands() {
		switch child.Name() {
		case "pod", "node":
			command.RemoveCommand(child)
		}
	}
	command.AddCommand(
		newTopPodCommand(factory, streams, namespace),
		newTopNodeCommand(factory, streams),
	)
	return command
}

func newTopPodCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) *cobra.Command {
	options := &topcmd.TopPodOptions{
		IOStreams:          streams,
		UseProtocolBuffers: true,
	}
	command := topcmd.NewCmdTopPod(factory, options, streams)
	command.Run = func(command *cobra.Command, args []string) {
		cmdutil.CheckErr(options.Complete(factory, command, args))
		discoveryClient, err := factory.ToDiscoveryClient()
		cmdutil.CheckErr(err)
		options.DiscoveryClient = discoveryClient
		cmdutil.CheckErr(options.Validate())
		cmdutil.CheckErr(options.RunTopPod())
	}
	addNamespaceFlag(command, namespace)
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

func newTopNodeCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) *cobra.Command {
	options := &topcmd.TopNodeOptions{
		IOStreams:          streams,
		UseProtocolBuffers: true,
	}
	command := topcmd.NewCmdTopNode(factory, options, streams)
	command.Run = func(command *cobra.Command, args []string) {
		cmdutil.CheckErr(options.Complete(factory, command, args))
		discoveryClient, err := factory.ToDiscoveryClient()
		cmdutil.CheckErr(err)
		options.DiscoveryClient = discoveryClient
		cmdutil.CheckErr(options.Validate())
		cmdutil.CheckErr(options.RunTopNode())
	}
	return command
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
