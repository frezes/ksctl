package cmd

import (
	"flag"
	"io"

	"github.com/kubesphere/ksctl/pkg/auth"
	clientkubernetes "github.com/kubesphere/ksctl/pkg/client/kubernetes"
	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/klog/v2"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

func NewRootCommand(streams IOStreams, info VersionInfo) *cobra.Command {
	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.ErrOut == nil {
		streams.ErrOut = io.Discard
	}

	cmd := &cobra.Command{
		Use:           "ksctl",
		Short:         "KubeSphere command line tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	if streams.In != nil {
		cmd.SetIn(streams.In)
	}

	connection := &clientkubernetes.Options{UserAgent: "ksctl/" + info.Version}
	cmd.PersistentFlags().StringVar(&connection.Endpoint, "endpoint", "", "KubeSphere API endpoint")
	cmd.PersistentFlags().StringVar(&connection.Token, "token", "", "KubeSphere bearer token")
	cmd.PersistentFlags().StringVar(&connection.Context, "context", "", "ksctl context name")
	cmd.PersistentFlags().StringVar(&connection.Cluster, "cluster", "", "KubeSphere cluster name")
	cmd.PersistentFlags().StringVar(&connection.Workspace, "workspace", "", "KubeSphere workspace name")
	cmd.PersistentFlags().StringVarP(&connection.Namespace, "namespace", "n", "", "Kubernetes namespace or KubeSphere project")
	cmd.PersistentFlags().StringVar(&connection.RequestTimeout, "request-timeout", "0", "The length of time to wait before giving up on a single server request")
	cmd.PersistentFlags().BoolVar(&connection.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "Skip the validity check for the server's certificate")
	cmd.PersistentFlags().BoolVar(&connection.NoInteractive, "no-interactive", false, "Fail instead of prompting for missing input")
	addKlogVerbosityFlag(cmd, streams.ErrOut)

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print client version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(info.PrintHuman()))
			return err
		},
	})

	cmd.AddCommand(newConfigCommand(streams))
	oauth := auth.NewOAuth(clientkubesphere.NewRESTClientFactory(nil))
	cmd.AddCommand(newAuthCommand(connection.UserAgent, oauth))

	provider := auth.NewProvider(auth.ProviderOptions{Refresher: oauth})
	getter := clientkubernetes.NewRESTClientGetter(connection, clientkubernetes.Dependencies{
		TokenProvider: provider,
	})
	factory := cmdutil.NewFactory(getter)
	kubeStreams := genericiooptions.IOStreams{
		In:     streams.In,
		Out:    streams.Out,
		ErrOut: streams.ErrOut,
	}
	cmd.AddCommand(get.NewCmdGet(cmd.Use, factory, kubeStreams))
	cmd.AddCommand(describecmd.NewCmdDescribe(cmd.Use, factory, kubeStreams))

	return cmd
}

func addKlogVerbosityFlag(cmd *cobra.Command, errOut io.Writer) {
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klogFlags.SetOutput(io.Discard)
	klog.InitFlags(klogFlags)
	if verbosity := klogFlags.Lookup("v"); verbosity != nil {
		cmd.PersistentFlags().AddGoFlag(verbosity)
	}
	klog.SetOutput(errOut)
	klog.LogToStderr(false)
}
