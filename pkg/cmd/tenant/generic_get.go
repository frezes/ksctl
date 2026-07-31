package tenant

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	utilcomp "k8s.io/kubectl/pkg/util/completion"
)

func newGenericGetCommand(
	parent string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) *cobra.Command {
	options := getcmd.NewGetOptions(parent, streams)
	command := &cobra.Command{
		Use: fmt.Sprintf(
			"get [(-o|--output=)%s] (TYPE [NAME | -l label] | TYPE/NAME ...) [flags]",
			strings.Join(options.PrintFlags.AllowedFormats(), "|"),
		),
		DisableFlagsInUseLine: true,
		Short:                 "Display KubeSphere tenant resources or arbitrary Kubernetes resources",
		RunE: func(command *cobra.Command, args []string) error {
			if err := options.Complete(factory, command, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(factory, args)
		},
	}
	command.ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(factory)

	options.PrintFlags.AddFlags(command)
	command.Flags().StringVar(
		&options.Raw,
		"raw",
		options.Raw,
		"Raw URI to request from the server using the configured transport",
	)
	command.Flags().BoolVarP(
		&options.Watch,
		"watch",
		"w",
		options.Watch,
		"After listing/getting the requested object, watch for changes",
	)
	command.Flags().BoolVar(
		&options.WatchOnly,
		"watch-only",
		options.WatchOnly,
		"Watch for changes without an initial list",
	)
	command.Flags().BoolVar(
		&options.OutputWatchEvents,
		"output-watch-events",
		options.OutputWatchEvents,
		"Output watch event objects",
	)
	command.Flags().BoolVar(
		&options.IgnoreNotFound,
		"ignore-not-found",
		options.IgnoreNotFound,
		"Suppress NotFound errors for named objects",
	)
	command.Flags().StringVar(
		&options.FieldSelector,
		"field-selector",
		options.FieldSelector,
		"Selector (field query) to filter on",
	)
	command.Flags().BoolVarP(
		&options.AllNamespaces,
		"all-namespaces",
		"A",
		options.AllNamespaces,
		"List the requested object(s) across all namespaces",
	)
	if namespace == nil {
		namespace = new(string)
	}
	command.Flags().StringVarP(
		namespace,
		"namespace",
		"n",
		*namespace,
		"Kubernetes namespace or KubeSphere project",
	)
	command.Flags().BoolVar(
		&options.ServerPrint,
		"server-print",
		options.ServerPrint,
		"Have the server return table output",
	)
	cmdutil.AddFilenameOptionFlags(
		command,
		&options.FilenameOptions,
		"identifying the resource to get from a server",
	)
	cmdutil.AddChunkSizeFlag(command, &options.ChunkSize)
	cmdutil.AddLabelSelectorFlagVar(command, &options.LabelSelector)
	cmdutil.AddSubresourceFlags(
		command,
		&options.Subresource,
		"If specified, gets the subresource of the requested object",
	)
	return command
}
