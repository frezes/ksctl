package cmd

import (
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/annotate"
	"k8s.io/kubectl/pkg/cmd/apiresources"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/attach"
	kubectlauth "k8s.io/kubectl/pkg/cmd/auth"
	"k8s.io/kubectl/pkg/cmd/autoscale"
	"k8s.io/kubectl/pkg/cmd/certificates"
	"k8s.io/kubectl/pkg/cmd/clusterinfo"
	"k8s.io/kubectl/pkg/cmd/cp"
	"k8s.io/kubectl/pkg/cmd/create"
	"k8s.io/kubectl/pkg/cmd/debug"
	deletecmd "k8s.io/kubectl/pkg/cmd/delete"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/diff"
	"k8s.io/kubectl/pkg/cmd/drain"
	"k8s.io/kubectl/pkg/cmd/edit"
	"k8s.io/kubectl/pkg/cmd/events"
	cmdexec "k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/explain"
	"k8s.io/kubectl/pkg/cmd/expose"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	"k8s.io/kubectl/pkg/cmd/kustomize"
	"k8s.io/kubectl/pkg/cmd/label"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	"k8s.io/kubectl/pkg/cmd/patch"
	"k8s.io/kubectl/pkg/cmd/portforward"
	"k8s.io/kubectl/pkg/cmd/proxy"
	"k8s.io/kubectl/pkg/cmd/replace"
	"k8s.io/kubectl/pkg/cmd/rollout"
	"k8s.io/kubectl/pkg/cmd/run"
	"k8s.io/kubectl/pkg/cmd/scale"
	"k8s.io/kubectl/pkg/cmd/set"
	"k8s.io/kubectl/pkg/cmd/taint"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/cmd/wait"
	utilcomp "k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"
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
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.Flags().VarP(
		&deferredHelpValue{},
		"help",
		"h",
		"help for kube",
	)
	command.Flags().Lookup("help").NoOptDefVal = "true"
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

	getCommand := getcmd.NewCmdGet(kubeDisplayName, factory, streams)
	getCommand.ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(factory)
	debugCommand := debug.NewCmdDebug(factory, streams)
	debugCommand.ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(factory)

	groups := templates.CommandGroups{
		{
			Message: "Basic Commands (Beginner):",
			Commands: []*cobra.Command{
				create.NewCmdCreate(factory, streams),
				expose.NewCmdExposeService(factory, streams),
				run.NewCmdRun(factory, streams),
				set.NewCmdSet(factory, streams),
			},
		},
		{
			Message: "Basic Commands (Intermediate):",
			Commands: []*cobra.Command{
				explain.NewCmdExplain(kubeDisplayName, factory, streams),
				getCommand,
				edit.NewCmdEdit(factory, streams),
				deletecmd.NewCmdDelete(factory, streams),
			},
		},
		{
			Message: "Deploy Commands:",
			Commands: []*cobra.Command{
				rollout.NewCmdRollout(kubeDisplayName, factory, streams),
				scale.NewCmdScale(factory, streams),
				autoscale.NewCmdAutoscale(factory, streams),
			},
		},
		{
			Message: "Cluster Management Commands:",
			Commands: []*cobra.Command{
				certificates.NewCmdCertificate(factory, streams),
				clusterinfo.NewCmdClusterInfo(factory, streams),
				newKubeTopCommand(factory, streams),
				drain.NewCmdCordon(factory, streams),
				drain.NewCmdUncordon(factory, streams),
				drain.NewCmdDrain(factory, streams),
				taint.NewCmdTaint(factory, streams),
			},
		},
		{
			Message: "Troubleshooting and Debugging Commands:",
			Commands: []*cobra.Command{
				describecmd.NewCmdDescribe(kubeDisplayName, factory, streams),
				logscmd.NewCmdLogs(factory, streams),
				attach.NewCmdAttach(factory, streams),
				cmdexec.NewCmdExec(factory, streams),
				portforward.NewCmdPortForward(factory, streams),
				proxy.NewCmdProxy(factory, streams),
				cp.NewCmdCp(factory, streams),
				kubectlauth.NewCmdAuth(factory, streams),
				debugCommand,
				events.NewCmdEvents(factory, streams),
			},
		},
		{
			Message: "Advanced Commands:",
			Commands: []*cobra.Command{
				diff.NewCmdDiff(factory, streams),
				apply.NewCmdApply(kubeDisplayName, factory, streams),
				patch.NewCmdPatch(factory, streams),
				replace.NewCmdReplace(factory, streams),
				wait.NewCmdWait(factory, streams),
				kustomize.NewCmdKustomize(streams),
			},
		},
		{
			Message: "Settings Commands:",
			Commands: []*cobra.Command{
				label.NewCmdLabel(factory, streams),
				annotate.NewCmdAnnotate(kubeDisplayName, factory, streams),
			},
		},
		{
			Message: "Discovery Commands:",
			Commands: []*cobra.Command{
				apiresources.NewCmdAPIVersions(factory, streams),
				apiresources.NewCmdAPIResources(factory, streams),
			},
		},
	}
	groups.Add(command)
	templates.ActsAsRootCommand(command, nil, groups...).ExposeFlags(
		command,
		"namespace",
		"request-timeout",
		"cluster",
		"context",
		"endpoint",
		"token",
		"v",
	)
	adaptNestedKubectlHelp(command, kubeDisplayName)
	rewriteKubectlExamples(command, kubeDisplayName)
	return command
}

type deferredHelpValue struct{}

// deferredHelpValue keeps Cobra from handling --help before validating
// positional arguments. A kubectl-style command embedded below another Cobra
// root would otherwise treat an excluded command name as an argument and
// successfully print the kube parent help.
func (*deferredHelpValue) Set(value string) error {
	_, err := strconv.ParseBool(value)
	return err
}

func (*deferredHelpValue) String() string {
	return "false"
}

func (*deferredHelpValue) Type() string {
	return "bool"
}

func (*deferredHelpValue) IsBoolFlag() bool {
	return true
}

// adaptNestedKubectlHelp removes assumptions made by kubectl's root-only help
// template when the same operation tree is mounted below ksctl kube.
func adaptNestedKubectlHelp(command *cobra.Command, displayName string) {
	upstreamHelp := command.HelpFunc()
	command.SetHelpFunc(func(current *cobra.Command, args []string) {
		destination := current.OutOrStdout()
		var output strings.Builder
		current.SetOut(&output)
		upstreamHelp(current, args)
		current.SetOut(destination)

		help := strings.ReplaceAll(
			output.String(),
			`Use "kube `,
			`Use "`+displayName+` `,
		)
		var filtered strings.Builder
		for _, line := range strings.SplitAfter(help, "\n") {
			if strings.Contains(line, "for a list of global command-line options") {
				continue
			}
			filtered.WriteString(line)
		}
		_, _ = io.WriteString(destination, filtered.String())
	})
}
