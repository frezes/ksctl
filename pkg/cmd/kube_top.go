package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	topcmd "k8s.io/kubectl/pkg/cmd/top"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newKubeTopCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) *cobra.Command {
	command := topcmd.NewCmdTop(factory, streams)
	for _, child := range command.Commands() {
		switch child.Name() {
		case "pod", "node":
			command.RemoveCommand(child)
		}
	}
	command.AddCommand(
		newKubeTopPodCommand(factory, streams),
		newKubeTopNodeCommand(factory, streams),
	)
	return command
}

func newKubeTopPodCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
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
	return command
}

func newKubeTopNodeCommand(
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
