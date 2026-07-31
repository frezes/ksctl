package tenant

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	utilcomp "k8s.io/kubectl/pkg/util/completion"
	kubesphererest "kubesphere.io/client-go/rest"
)

func newGenericGetCommand(
	parent string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
	state *aggregationState,
) *cobra.Command {
	stdout := newConditionalWriter(streams.Out, state)
	stderr := newConditionalWriter(streams.ErrOut, state)
	conditionalStreams := streams
	conditionalStreams.Out = stdout
	conditionalStreams.ErrOut = stderr
	options := getcmd.NewGetOptions(parent, conditionalStreams)
	command := &cobra.Command{
		Use: fmt.Sprintf(
			"get [(-o|--output=)%s] (TYPE [NAME | -l label] | TYPE/NAME ...) [flags]",
			strings.Join(options.PrintFlags.AllowedFormats(), "|"),
		),
		DisableFlagsInUseLine: true,
		Short:                 "Display KubeSphere tenant resources or arbitrary Kubernetes resources",
		RunE: func(command *cobra.Command, args []string) error {
			state.mode = aggregateDisabled
			state.workspace = ""
			state.used.Store(false)
			stdout.Reset()
			stderr.Reset()

			workspace, err := command.Flags().GetString("workspace")
			if err != nil {
				return err
			}
			if err := validateGenericScope(
				command,
				factory,
				options,
				args,
				workspace,
			); err != nil {
				return err
			}
			switch {
			case workspace != "":
				state.mode = aggregateWorkspace
				state.workspace = workspace
				state.used.Store(true)
				options.AllNamespaces = true
			case options.AllNamespaces:
				state.mode = aggregateOnForbidden
			}
			if err := options.Complete(factory, command, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			if err := options.Run(factory, args); err != nil {
				return err
			}
			if err := stdout.Commit(); err != nil {
				return err
			}
			return stderr.Commit()
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

func validateGenericScope(
	command *cobra.Command,
	factory cmdutil.Factory,
	options *getcmd.GetOptions,
	args []string,
	workspace string,
) error {
	if workspace == "" {
		return nil
	}
	if messages := kubesphererest.IsValidPathSegmentName(workspace); len(messages) != 0 {
		return fmt.Errorf("invalid workspace %q: %v", workspace, messages)
	}
	if command.Flags().Changed("namespace") {
		return fmt.Errorf("--workspace and --namespace cannot be used together")
	}
	if options.AllNamespaces {
		return fmt.Errorf("--workspace and --all-namespaces cannot be used together")
	}
	if options.Watch || options.WatchOnly {
		return fmt.Errorf(
			"watch is not supported with --workspace; choose one namespace with --namespace",
		)
	}
	if options.Raw != "" {
		return fmt.Errorf("--workspace and --raw cannot be used together")
	}
	if !cmdutil.IsFilenameSliceEmpty(options.Filenames, options.Kustomize) {
		return fmt.Errorf("--workspace and --filename cannot be used together")
	}
	if len(args) != 1 || strings.Contains(args[0], "/") {
		return fmt.Errorf("--workspace supports collection queries only")
	}

	mapper, err := factory.ToRESTMapper()
	if err != nil {
		return err
	}
	for _, resourceName := range strings.Split(args[0], ",") {
		mapping, err := mappingForResourceArg(mapper, resourceName)
		if err != nil {
			return err
		}
		if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
			return fmt.Errorf(
				"resource %s is cluster-scoped and cannot be filtered by workspace %q",
				mapping.Resource.Resource,
				workspace,
			)
		}
	}
	return nil
}

func mappingForResourceArg(
	mapper meta.RESTMapper,
	resourceArg string,
) (*meta.RESTMapping, error) {
	fullySpecified, groupResource := schema.ParseResourceArg(resourceArg)
	if fullySpecified != nil {
		if kind, err := mapper.KindFor(*fullySpecified); err == nil {
			if mapping, err := mapper.RESTMapping(
				kind.GroupKind(),
				kind.Version,
			); err == nil {
				return mapping, nil
			}
		}
	}
	kind, err := mapper.KindFor(groupResource.WithVersion(""))
	if err != nil {
		return nil, err
	}
	return mapper.RESTMapping(kind.GroupKind(), kind.Version)
}

type conditionalWriter struct {
	delegate io.Writer
	state    *aggregationState

	mu        sync.Mutex
	buffer    bytes.Buffer
	committed bool
}

func newConditionalWriter(
	delegate io.Writer,
	state *aggregationState,
) *conditionalWriter {
	if delegate == nil {
		delegate = io.Discard
	}
	return &conditionalWriter{delegate: delegate, state: state}
}

func (w *conditionalWriter) Write(p []byte) (int, error) {
	if w.state == nil || !w.state.used.Load() {
		return w.delegate.Write(p)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(p)
}

func (w *conditionalWriter) Commit() error {
	if w.state == nil || !w.state.used.Load() {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		return nil
	}
	if _, err := io.Copy(w.delegate, bytes.NewReader(w.buffer.Bytes())); err != nil {
		return fmt.Errorf("write tenant get output: %w", err)
	}
	w.committed = true
	return nil
}

func (w *conditionalWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer.Reset()
	w.committed = false
}
