package tenant

import (
	"context"
	"fmt"
	"io"
	"time"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	"github.com/spf13/cobra"
	kubesphererest "kubesphere.io/client-go/rest"
)

type RESTClientGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
	KubeSphereCluster() (string, error)
}

func NewCommand(getter RESTClientGetter) *cobra.Command {
	command := &cobra.Command{
		Use:   "tenant",
		Short: "Inspect KubeSphere tenant resources",
	}
	get := &cobra.Command{
		Use:   "get",
		Short: "Display KubeSphere tenant resources",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("tenant resource is required")
		},
	}

	var output string
	get.PersistentFlags().StringVarP(&output, "output", "o", "table", "Output format: table, json, or yaml")

	workspace := &cobra.Command{
		Use:     "workspace [NAME]",
		Aliases: []string{"workspaces"},
		Short:   "Display KubeSphere workspaces",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runGet(cmd.Context(), cmd.OutOrStdout(), getter, Request{
				Resource: ResourceWorkspace,
				Name:     name,
			}, output)
		},
	}

	var namespaceWorkspace string
	namespace := &cobra.Command{
		Use:     "namespace",
		Aliases: []string{"namespaces", "ns"},
		Short:   "Display KubeSphere namespaces",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGet(cmd.Context(), cmd.OutOrStdout(), getter, Request{
				Resource:  ResourceNamespace,
				Workspace: namespaceWorkspace,
			}, output)
		},
	}
	namespace.Flags().StringVar(&namespaceWorkspace, "workspace", "", "KubeSphere workspace name")

	var clusterWorkspace string
	cluster := &cobra.Command{
		Use:     "cluster",
		Aliases: []string{"clusters"},
		Short:   "Display KubeSphere clusters",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGet(cmd.Context(), cmd.OutOrStdout(), getter, Request{
				Resource:  ResourceCluster,
				Workspace: clusterWorkspace,
			}, output)
		},
	}
	cluster.Flags().StringVar(&clusterWorkspace, "workspace", "", "KubeSphere workspace name")

	get.AddCommand(workspace, namespace, cluster)
	command.AddCommand(get)
	return command
}

func runGet(ctx context.Context, out io.Writer, getter RESTClientGetter, request Request, output string) error {
	if err := validateOutput(output); err != nil {
		return err
	}
	if err := validateInput(request); err != nil {
		return err
	}
	if getter == nil {
		return fmt.Errorf("KubeSphere REST client getter is required")
	}
	if request.Resource == ResourceNamespace {
		cluster, err := getter.KubeSphereCluster()
		if err != nil {
			return fmt.Errorf("resolve tenant cluster: %w", err)
		}
		request.Cluster = cluster
	}
	config, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("resolve tenant connection: %w", err)
	}
	restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(config)
	if err != nil {
		return fmt.Errorf("build tenant client: %w", err)
	}
	response, err := NewClient(restClient).Get(ctx, request)
	if err != nil {
		return err
	}
	return printResponse(out, output, request.Resource, response, time.Now())
}

func validateOutput(output string) error {
	switch output {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("unsupported output format %q: must be table, json, or yaml", output)
	}
}

func validateInput(request Request) error {
	for label, value := range map[string]string{
		"workspace name": request.Name,
		"workspace":      request.Workspace,
	} {
		if value == "" {
			continue
		}
		if messages := kubesphererest.IsValidPathSegmentName(value); len(messages) != 0 {
			return fmt.Errorf("invalid %s %q: %v", label, value, messages)
		}
	}
	return nil
}
