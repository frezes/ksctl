package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kubesphererest "kubesphere.io/client-go/rest"
	clientkubesphere "kubesphere.io/ksctl/pkg/client/kubesphere"
	extensioncmd "kubesphere.io/ksctl/pkg/cmd/extension"
	kubesphereextension "kubesphere.io/ksctl/pkg/kubesphere/extension"
)

type extensionRESTConfigGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
}

type extensionRESTClientFactory interface {
	ForConfig(
		*kubesphererest.Config,
	) (kubesphererest.Interface, error)
}

func newExtensionCommand(
	parent string,
	streams IOStreams,
	getter extensionRESTConfigGetter,
	factory extensionRESTClientFactory,
) *cobra.Command {
	return extensioncmd.NewCommand(
		parent,
		genericiooptions.IOStreams{
			In:     streams.In,
			Out:    streams.Out,
			ErrOut: streams.ErrOut,
		},
		func() (extensioncmd.Service, error) {
			config, err := getter.ToRESTConfig()
			if err != nil {
				return nil, fmt.Errorf(
					"resolve KubeSphere connection: %w",
					err,
				)
			}
			restClient, err := factory.ForConfig(config)
			if err != nil {
				return nil, fmt.Errorf(
					"create KubeSphere REST client: %w",
					err,
				)
			}
			return kubesphereextension.NewService(
				kubesphereextension.NewRESTClient(restClient),
			), nil
		},
	)
}

var _ extensionRESTClientFactory = (*clientkubesphere.RESTClientFactory)(nil)
