package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	kubesphererest "kubesphere.io/client-go/rest"
)

var version = "dev"

type VersionInfo struct {
	Version string
}

type serverVersionInfo struct {
	KubeSphere string
	Kubernetes string
}

type versionRESTClientGetter interface {
	ToRESTConfig() (*rest.Config, error)
	KubeSphereCluster() (string, error)
}

func DefaultVersionInfo() VersionInfo {
	return VersionInfo{Version: version}
}

func newVersionCommand(info VersionInfo, getter versionRESTClientGetter) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print client and server version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverInfo, err := loadServerVersion(cmd.Context(), getter)
			if err != nil {
				serverInfo = serverVersionInfo{}
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), info.PrintHuman(serverInfo))
			return err
		},
	}
}

func loadServerVersion(ctx context.Context, getter versionRESTClientGetter) (serverVersionInfo, error) {
	cluster, err := getter.KubeSphereCluster()
	if err != nil {
		return serverVersionInfo{}, err
	}
	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		return serverVersionInfo{}, err
	}
	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		return serverVersionInfo{}, err
	}
	client, err := clientkubesphere.NewRESTClientFactory(httpClient).ForConfig(&kubesphererest.Config{
		Host:      restConfig.Host,
		UserAgent: restConfig.UserAgent,
		Timeout:   restConfig.Timeout,
	})
	if err != nil {
		return serverVersionInfo{}, err
	}

	request := client.Get().AbsPath("/kapis/version")
	if cluster != "" {
		request.Cluster(cluster)
	}
	raw, err := request.Do(ctx).Raw()
	if err != nil {
		return serverVersionInfo{}, err
	}
	var response struct {
		GitVersion string `json:"gitVersion"`
		Kubernetes struct {
			GitVersion string `json:"gitVersion"`
		} `json:"kubernetes"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return serverVersionInfo{}, err
	}
	return serverVersionInfo{
		KubeSphere: response.GitVersion,
		Kubernetes: response.Kubernetes.GitVersion,
	}, nil
}

func (v VersionInfo) PrintHuman(server serverVersionInfo) string {
	return fmt.Sprintf(
		"ksctl Version: %s\nKubeSphere Version: %s\nKubernetes Version: %s\n",
		valueOrUnknown(v.Version),
		valueOrUnknown(server.KubeSphere),
		valueOrUnknown(server.Kubernetes),
	)
}

func valueOrUnknown(value string) string {
	if strings.ContainsFunc(value, unicode.IsControl) {
		return "unknown"
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
