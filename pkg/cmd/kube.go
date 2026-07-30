package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newKubeCommand(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace, requestTimeout *string,
) *cobra.Command {
	kubeDisplayName := displayName + " kube"
	command := &cobra.Command{
		Use:   "kube",
		Short: "Manage Kubernetes resources through KubeSphere",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.PersistentFlags().StringVarP(
		namespace,
		"namespace",
		"n",
		"",
		"Kubernetes namespace or KubeSphere project",
	)
	command.PersistentFlags().StringVar(
		requestTimeout,
		"request-timeout",
		"0",
		"The length of time to wait before giving up on a single server request",
	)
	command.AddCommand(
		getcmd.NewCmdGet(kubeDisplayName, factory, streams),
		describecmd.NewCmdDescribe(kubeDisplayName, factory, streams),
		logscmd.NewCmdLogs(factory, streams),
		newKubeTopCommand(factory, streams),
	)
	rewriteKubectlExamples(command, kubeDisplayName)
	return command
}
