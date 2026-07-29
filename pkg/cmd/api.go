package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kubesphererest "kubesphere.io/client-go/rest"
)

type apiRESTConfigGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
}

type apiRESTClientFactory interface {
	ForConfig(*kubesphererest.Config) (kubesphererest.Interface, error)
}

type apiRequestOptions struct {
	method    string
	data      string
	methodSet bool
	dataSet   bool
}

func newAPICommand(getter apiRESTConfigGetter, factory apiRESTClientFactory) *cobra.Command {
	options := apiRequestOptions{method: http.MethodGet}
	command := &cobra.Command{
		Use:   "api API_PATH",
		Short: "Send a request to a KubeSphere API path",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			options.methodSet = command.Flags().Changed("method")
			options.dataSet = command.Flags().Changed("data")
			return runAPI(
				command.Context(),
				command.OutOrStdout(),
				getter,
				factory,
				args[0],
				options,
			)
		},
	}
	command.Flags().StringVarP(&options.method, "method", "X", http.MethodGet, "HTTP request method")
	command.Flags().StringVarP(&options.data, "data", "d", "", "Inline JSON request body")
	return command
}

func runAPI(
	ctx context.Context,
	out io.Writer,
	getter apiRESTConfigGetter,
	factory apiRESTClientFactory,
	apiPath string,
	options apiRequestOptions,
) error {
	method, err := normalizeAPIMethod(options)
	if err != nil {
		return err
	}
	if err := validateAPIPath(apiPath); err != nil {
		return err
	}
	if getter == nil {
		return fmt.Errorf("KubeSphere REST config getter is required")
	}
	if factory == nil {
		return fmt.Errorf("KubeSphere REST client factory is required")
	}

	config, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("resolve API connection: %w", err)
	}
	client, err := factory.ForConfig(config)
	if err != nil {
		return fmt.Errorf("create KubeSphere REST client: %w", err)
	}

	request := client.Verb(method).RequestURI(apiPath)
	if options.dataSet {
		request.SetHeader("Content-Type", "application/json")
		request.Body([]byte(options.data))
	}
	raw, requestErr := request.DoRaw(ctx)
	var status apierrors.APIStatus
	if errors.As(requestErr, &status) {
		statusCode := status.Status().Code
		if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
			requestErr = nil
		}
	}

	var writeErr error
	if len(raw) > 0 {
		_, writeErr = io.Copy(out, bytes.NewReader(raw))
		if writeErr != nil {
			writeErr = fmt.Errorf("write API response: %w", writeErr)
		}
	}
	if requestErr != nil {
		requestErr = fmt.Errorf("request KubeSphere API %q: %w", apiPath, requestErr)
	}
	return errors.Join(requestErr, writeErr)
}

func normalizeAPIMethod(options apiRequestOptions) (string, error) {
	method := strings.TrimSpace(options.method)
	if !options.methodSet {
		if options.dataSet {
			method = http.MethodPost
		} else {
			method = http.MethodGet
		}
	}
	if method == "" {
		return "", fmt.Errorf("HTTP method must not be empty")
	}
	method = strings.ToUpper(method)
	if _, err := http.NewRequest(method, "http://localhost", nil); err != nil {
		return "", fmt.Errorf("invalid HTTP method %q: %w", method, err)
	}
	return method, nil
}

func validateAPIPath(apiPath string) error {
	if strings.Contains(apiPath, "#") {
		return fmt.Errorf("invalid API path %q: fragments are not supported", apiPath)
	}
	parsed, err := url.ParseRequestURI(apiPath)
	if err != nil {
		if !strings.HasPrefix(apiPath, "/") {
			return fmt.Errorf("API path %q must begin with /", apiPath)
		}
		return fmt.Errorf("invalid API path %q: %w", apiPath, err)
	}
	if parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(apiPath, "//") {
		return fmt.Errorf("API path %q must be server-relative", apiPath)
	}
	if !strings.HasPrefix(apiPath, "/") {
		return fmt.Errorf("API path %q must begin with /", apiPath)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("invalid API path %q: fragments are not supported", apiPath)
	}
	return nil
}
