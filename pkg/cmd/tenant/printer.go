package tenant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/cli-runtime/pkg/printers"
	"sigs.k8s.io/yaml"
)

func printResponse(out io.Writer, format string, resource Resource, response Response, now time.Time) error {
	switch format {
	case "json":
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, response.Raw, "", "  "); err != nil {
			return fmt.Errorf("format tenant JSON output: %w", err)
		}
		formatted.WriteByte('\n')
		if _, err := io.Copy(out, &formatted); err != nil {
			return fmt.Errorf("write tenant JSON output: %w", err)
		}
		return nil
	case "yaml":
		data, err := yaml.JSONToYAML(response.Raw)
		if err != nil {
			return fmt.Errorf("format tenant YAML output: %w", err)
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
		if _, err := out.Write(data); err != nil {
			return fmt.Errorf("write tenant YAML output: %w", err)
		}
		return nil
	case "table":
		return printTable(out, resource, response.Objects, now)
	default:
		return fmt.Errorf("unsupported output format %q: must be table, json, or yaml", format)
	}
}

func printTable(out io.Writer, resource Resource, objects []map[string]any, now time.Time) error {
	if len(objects) == 0 {
		if _, err := fmt.Fprintln(out, "No resources found"); err != nil {
			return fmt.Errorf("write tenant table output: %w", err)
		}
		return nil
	}

	writer := printers.GetNewTabWriter(out)
	switch resource {
	case ResourceWorkspace:
		_, _ = fmt.Fprintln(writer, "NAME\tCLUSTERS\tADMINISTRATOR\tAGE")
		for _, object := range objects {
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
				nestedString(object, "metadata", "name"),
				workspaceClusters(object),
				nestedString(object, "spec", "template", "spec", "manager"),
				objectAge(object, now),
			)
		}
	case ResourceNamespace:
		_, _ = fmt.Fprintln(writer, "NAME\tSTATUS\tAGE")
		for _, object := range objects {
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n",
				nestedString(object, "metadata", "name"),
				nestedString(object, "status", "phase"),
				objectAge(object, now),
			)
		}
	case ResourceCluster:
		_, _ = fmt.Fprintln(writer, "NAME\tPROVIDER\tVERSION")
		for _, object := range objects {
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n",
				nestedString(object, "metadata", "name"),
				nestedString(object, "spec", "provider"),
				nestedString(object, "status", "kubernetesVersion"),
			)
		}
	default:
		return fmt.Errorf("unsupported tenant resource %q", resource)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write tenant table output: %w", err)
	}
	return nil
}

func nestedString(object map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(object, fields...)
	if err != nil || !found {
		return ""
	}
	return value
}

func workspaceClusters(object map[string]any) string {
	clusters, found, err := unstructured.NestedSlice(object, "spec", "placement", "clusters")
	if err != nil || !found {
		return ""
	}
	names := make([]string, 0, len(clusters))
	for _, item := range clusters {
		cluster, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name := nestedString(cluster, "name"); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

func objectAge(object map[string]any, now time.Time) string {
	created, err := time.Parse(time.RFC3339, nestedString(object, "metadata", "creationTimestamp"))
	if err != nil {
		return "<unknown>"
	}
	return duration.HumanDuration(now.Sub(created))
}
