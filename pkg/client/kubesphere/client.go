package kubesphere

import (
	"fmt"
	"net/http"

	kubespherescheme "kubesphere.io/client-go/kubesphere/scheme"
	kubesphererest "kubesphere.io/client-go/rest"
)

type RESTClientFactory struct {
	httpClient *http.Client
}

func NewRESTClientFactory(httpClient *http.Client) *RESTClientFactory {
	return &RESTClientFactory{httpClient: httpClient}
}

func (f *RESTClientFactory) ForConfig(config *kubesphererest.Config) (kubesphererest.Interface, error) {
	if config == nil {
		return nil, fmt.Errorf("KubeSphere REST config is required")
	}

	copied := kubesphererest.CopyConfig(config)
	if f == nil || f.httpClient == nil {
		return kubesphererest.UnversionedRESTClientFor(copied)
	}
	if copied.NegotiatedSerializer == nil {
		copied.NegotiatedSerializer = kubespherescheme.Codecs.WithoutConversion()
	}
	httpClient := *f.httpClient
	if copied.WrapTransport != nil {
		transport := httpClient.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		httpClient.Transport = copied.WrapTransport(transport)
	}
	return kubesphererest.UnversionedRESTClientForConfigAndClient(copied, &httpClient)
}
