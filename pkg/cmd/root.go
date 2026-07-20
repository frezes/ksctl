package cmd

import (
	"flag"
	"io"
	"strings"
	"sync"

	"github.com/kubesphere/ksctl/pkg/auth"
	clientkubernetes "github.com/kubesphere/ksctl/pkg/client/kubernetes"
	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	plugincmd "github.com/kubesphere/ksctl/pkg/cmd/plugin"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/klog/v2"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	kubectli18n "k8s.io/kubectl/pkg/util/i18n"
)

var (
	loadEnglishTranslationsOnce sync.Once
	loadEnglishTranslationsErr  error
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

func NewRootCommand(streams IOStreams, info VersionInfo) *cobra.Command {
	return newRootCommand("ksctl", "", streams, info)
}

func NewKubectlPluginCommand(streams IOStreams, info VersionInfo) *cobra.Command {
	return newRootCommand("kubectl-ks", "kubectl ks", streams, info)
}

func newRootCommand(use, displayName string, streams IOStreams, info VersionInfo) *cobra.Command {
	loadEnglishTranslationsOnce.Do(func() {
		loadEnglishTranslationsErr = kubectli18n.LoadTranslations("kubectl", func() string { return "default" })
	})
	if loadEnglishTranslationsErr != nil {
		panic(loadEnglishTranslationsErr)
	}

	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.ErrOut == nil {
		streams.ErrOut = io.Discard
	}

	cmd := &cobra.Command{
		Use:           use,
		Short:         "KubeSphere command line tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	if displayName != "" {
		cmd.Annotations = map[string]string{
			cobra.CommandDisplayNameAnnotation: displayName,
		}
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
	cmd.PersistentFlags().StringVarP(&connection.Namespace, "namespace", "n", "", "Kubernetes namespace or KubeSphere project")
	cmd.PersistentFlags().StringVar(&connection.RequestTimeout, "request-timeout", "0", "The length of time to wait before giving up on a single server request")
	addKlogVerbosityFlag(cmd, streams.ErrOut)

	oauth := auth.NewOAuth(clientkubesphere.NewRESTClientFactory(nil))
	provider := auth.NewProvider(auth.ProviderOptions{Requester: oauth})
	getter := clientkubernetes.NewRESTClientGetter(connection, clientkubernetes.Dependencies{
		TokenProvider: provider,
	})

	cmd.AddCommand(newVersionCommand(info, getter))
	cmd.AddCommand(newConfigCommand(streams))
	cmd.AddCommand(newAuthCommand(connection.UserAgent, oauth))

	factory := cmdutil.NewFactory(getter)
	kubeStreams := genericiooptions.IOStreams{
		In:     streams.In,
		Out:    streams.Out,
		ErrOut: streams.ErrOut,
	}
	cmd.AddCommand(plugincmd.NewCommand(cmd.DisplayName(), kubeStreams))
	getCommand := get.NewCmdGet(cmd.DisplayName(), factory, kubeStreams)
	getCommand.Example = strings.ReplaceAll(getCommand.Example, "kubectl ", cmd.DisplayName()+" ")
	describeCommand := describecmd.NewCmdDescribe(cmd.DisplayName(), factory, kubeStreams)
	describeCommand.Example = strings.ReplaceAll(describeCommand.Example, "kubectl ", cmd.DisplayName()+" ")
	cmd.AddCommand(getCommand, describeCommand)

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
