package extension

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"k8s.io/cli-runtime/pkg/printers"
	kubesphereextension "kubesphere.io/ksctl/pkg/kubesphere/extension"
	"sigs.k8s.io/yaml"
)

type outputFormat string

const (
	outputTable outputFormat = "table"
	outputWide  outputFormat = "wide"
	outputJSON  outputFormat = "json"
	outputYAML  outputFormat = "yaml"
)

type rawJSONResult interface {
	RawJSON() []byte
}

func parseOutput(value string, allowWide bool) (outputFormat, error) {
	format := outputFormat(value)
	switch format {
	case outputTable, outputJSON, outputYAML:
		return format, nil
	case outputWide:
		if allowWide {
			return format, nil
		}
	}
	return "", fmt.Errorf(
		"unsupported output format %q; use table%s, json, or yaml",
		value,
		map[bool]string{true: ", wide"}[allowWide],
	)
}

func writeStructured(
	out io.Writer,
	result rawJSONResult,
	format outputFormat,
) error {
	data := bytes.TrimRight(result.RawJSON(), "\r\n")
	if format == outputYAML {
		converted, err := yaml.JSONToYAML(data)
		if err != nil {
			return fmt.Errorf("convert extension output to YAML: %w", err)
		}
		data = bytes.TrimRight(converted, "\r\n")
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func scalar(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
}

func pendingScalar(value string) string {
	if value == "" {
		return "<pending>"
	}
	return value
}

func localizedValue(values map[string]string) string {
	for _, key := range []string{"en", "en-US", "zh", "zh-CN"} {
		if value := values[key]; value != "" {
			return value
		}
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}

func localized(values map[string]string) string {
	return scalar(localizedValue(values))
}

func appendNonEmptyRow(rows *[][]string, field, value string) {
	if value != "" {
		*rows = append(*rows, []string{field, value})
	}
}

func printList(
	out io.Writer,
	result kubesphereextension.ListResult,
	format outputFormat,
) error {
	headers := []string{
		"NAME",
		"CATEGORY",
		"RECOMMENDED",
		"INSTALLED",
		"STATE",
	}
	if format == outputWide {
		headers = append(headers, "PROVIDER", "ENABLED")
	}
	rows := make([][]string, 0, len(result.Items)+1)
	rows = append(rows, headers)
	for _, item := range result.Items {
		extension := item.Extension.Value
		state := extension.Status.State
		installedVersion := extension.Status.InstalledVersion
		if item.InstallPlan != nil {
			plan := item.InstallPlan.Value
			state = plan.Status.State
			if successfulPlanState(plan.Status.State) &&
				plan.Status.Version != "" {
				installedVersion = plan.Status.Version
			}
		}
		row := []string{
			scalar(extension.Metadata.Name),
			scalar(extensionCategory(extension)),
			scalar(extension.Status.RecommendedVersion),
			scalar(installedVersion),
			scalar(state),
		}
		if format == outputWide {
			row = append(
				row,
				providerName(extension.Spec.Provider),
				optionalBool(extension.Status.Enabled),
			)
		}
		rows = append(rows, row)
	}
	return writeTable(out, rows)
}

func extensionCategory(extension kubesphereextension.Extension) string {
	if category := extension.Metadata.Labels["kubesphere.io/category"]; category != "" {
		return category
	}
	return extension.Spec.Category
}

func providerName(
	providers map[string]*kubesphereextension.Provider,
) string {
	provider := localizedProvider(providers)
	if provider == nil {
		return "<none>"
	}
	return scalar(provider.Name)
}

func localizedProvider(
	providers map[string]*kubesphereextension.Provider,
) *kubesphereextension.Provider {
	for _, key := range []string{"en", "en-US", "zh", "zh-CN"} {
		if provider := providers[key]; provider != nil {
			return provider
		}
	}
	for _, key := range slices.Sorted(maps.Keys(providers)) {
		if providers[key] != nil {
			return providers[key]
		}
	}
	return nil
}

func providerDetail(
	providers map[string]*kubesphereextension.Provider,
) string {
	provider := localizedProvider(providers)
	if provider == nil {
		return "<none>"
	}
	parts := make([]string, 0, 3)
	for _, value := range []string{
		provider.Name,
		provider.URL,
		provider.Email,
	} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "<none>"
	}
	return strings.Join(parts, ", ")
}

func optionalBool(value *bool) string {
	if value == nil {
		return "<none>"
	}
	return strconv.FormatBool(*value)
}

func printShow(
	out io.Writer,
	result kubesphereextension.ShowResult,
	format outputFormat,
) error {
	if result.SelectedVersion != nil {
		return printSelectedVersion(out, result)
	}
	extension := result.Extension.Value
	state := extension.Status.State
	enabled := optionalBool(extension.Status.Enabled)
	installedVersion := extension.Status.InstalledVersion
	targetVersion := ""
	if result.InstallPlan != nil {
		plan := result.InstallPlan.Value
		targetVersion = plan.Spec.Extension.Version
		state = plan.Status.State
		enabled = installPlanEnabled(plan)
		if successfulPlanState(plan.Status.State) &&
			plan.Status.Version != "" {
			installedVersion = plan.Status.Version
		}
	}

	rows := [][]string{{"FIELD", "VALUE"}}
	if format == outputWide {
		versionValues := make([]string, 0, len(result.Versions.Items))
		for _, version := range result.Versions.Items {
			versionValues = append(versionValues, version.Value.Spec.Version)
		}
		if len(versionValues) == 0 {
			for _, version := range extension.Status.Versions {
				versionValues = append(versionValues, version.Version)
			}
		}
		rows = append(rows,
			[]string{"Name", scalar(extension.Metadata.Name)},
			[]string{"Display Name", localized(extension.Spec.DisplayName)},
			[]string{"Description", localized(extension.Spec.Description)},
			[]string{"Category", scalar(extensionCategory(extension))},
			[]string{"Provider", providerDetail(extension.Spec.Provider)},
			[]string{"State", scalar(state)},
			[]string{"Enabled", enabled},
			[]string{"Installed Version", scalar(installedVersion)},
			[]string{"Target Version", scalar(targetVersion)},
			[]string{"Recommended Version", scalar(extension.Status.RecommendedVersion)},
			[]string{"Versions", stringList(versionValues)},
			[]string{"Conditions", formatConditions(extension.Status.Conditions)},
		)
	} else {
		appendNonEmptyRow(&rows, "Name", extension.Metadata.Name)
		appendNonEmptyRow(&rows, "Display Name", localizedValue(extension.Spec.DisplayName))
		appendNonEmptyRow(&rows, "Description", localizedValue(extension.Spec.Description))
		appendNonEmptyRow(&rows, "Category", extensionCategory(extension))
		appendNonEmptyRow(&rows, "State", state)
		appendNonEmptyRow(&rows, "Installed Version", installedVersion)
		appendNonEmptyRow(&rows, "Recommended Version", extension.Status.RecommendedVersion)
	}
	if err := writeTable(out, rows); err != nil {
		return err
	}
	if result.InstallPlan == nil {
		return nil
	}
	return printClusterSchedulingStatuses(
		out,
		result.InstallPlan.Value.Status.ClusterSchedulingStatuses,
		format,
	)
}

func printClusterSchedulingStatuses(
	out io.Writer,
	statuses map[string]kubesphereextension.InstallationStatus,
	format outputFormat,
) error {
	if len(statuses) == 0 {
		return nil
	}
	if _, err := io.WriteString(out, "\nclusterSchedulingStatuses\n\n"); err != nil {
		return err
	}
	headers := []string{"CLUSTER", "VERSION", "STATE"}
	if format == outputWide {
		headers = append(headers, "NAMESPACE", "JOB")
	}
	rows := [][]string{headers}
	for _, cluster := range slices.Sorted(maps.Keys(statuses)) {
		status := statuses[cluster]
		row := []string{
			cluster,
			scalar(status.Version),
			scalar(status.State),
		}
		if format == outputWide {
			row = append(
				row,
				scalar(status.TargetNamespace),
				scalar(status.JobName),
			)
		}
		rows = append(rows, row)
	}
	return writeTable(out, rows)
}

func successfulPlanState(state string) bool {
	return state == "Installed" || state == "Upgraded"
}

func printSelectedVersion(
	out io.Writer,
	result kubesphereextension.ShowResult,
) error {
	version := result.SelectedVersion.Value
	extensionName := version.Metadata.Labels["kubesphere.io/extension-ref"]
	if extensionName == "" {
		extensionName = result.Extension.Value.Metadata.Name
	}
	rows := [][]string{
		{"FIELD", "VALUE"},
		{"Name", scalar(version.Metadata.Name)},
		{"Extension", scalar(extensionName)},
		{"Version", scalar(version.Spec.Version)},
		{"Category", scalar(version.Spec.Category)},
		{"Installation Mode", scalar(version.Spec.InstallationMode)},
		{"Namespace", scalar(version.Spec.Namespace)},
		{"KubeSphere Version", scalar(version.Spec.KSVersion)},
		{"Kubernetes Version", scalar(version.Spec.KubeVersion)},
		{"Chart URL", scalar(version.Spec.ChartURL)},
		{"Dependencies", formatDependencies(version.Spec.ExternalDependencies)},
	}
	return writeTable(out, rows)
}

func stringList(values []string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		return "<none>"
	}
	return strings.Join(filtered, ", ")
}

func formatConditions(conditions []kubesphereextension.Condition) string {
	values := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		value := condition.Type + "=" + condition.Status
		if condition.Reason != "" {
			value += " " + condition.Reason
		}
		if condition.Message != "" {
			value += " " + condition.Message
		}
		values = append(values, strings.TrimSpace(value))
	}
	sort.Strings(values)
	return stringList(values)
}

func formatDependencies(
	dependencies []kubesphereextension.ExternalDependency,
) string {
	values := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		dependencyType := dependency.Type
		if dependencyType == "" {
			dependencyType = "extension"
		}
		values = append(values, fmt.Sprintf(
			"%s(type=%s version=%s required=%t)",
			scalar(dependency.Name),
			dependencyType,
			scalar(dependency.Version),
			dependency.Required,
		))
	}
	return stringList(values)
}

func printVersions(
	out io.Writer,
	result kubesphereextension.VersionsResult,
) error {
	rows := make([][]string, 0, len(result.Items.Items)+1)
	rows = append(rows, []string{
		"VERSION",
		"MODE",
		"KS-VERSION",
		"KUBE-VERSION",
		"NAMESPACE",
	})
	for _, item := range result.Items.Items {
		version := item.Value
		rows = append(rows, []string{
			scalar(version.Spec.Version),
			scalar(version.Spec.InstallationMode),
			scalar(version.Spec.KSVersion),
			scalar(version.Spec.KubeVersion),
			scalar(version.Spec.Namespace),
		})
	}
	return writeTable(out, rows)
}

func printStatus(
	out io.Writer,
	result kubesphereextension.StatusResult,
) error {
	rows := [][]string{{
		"NAME",
		"VERSION",
		"ENABLED",
		"STATE",
		"NAMESPACE",
		"JOB",
	}}
	if result.Object != nil {
		rows = append(rows, namedStatusRows(result.Object.Value)...)
	} else if result.List != nil {
		for _, item := range result.List.Items {
			rows = append(rows, hostStatusRow(item.Value))
		}
	}
	return writeTable(out, rows)
}

func namedStatusRows(plan kubesphereextension.InstallPlan) [][]string {
	rows := [][]string{hostStatusRow(plan)}
	for _, cluster := range slices.Sorted(
		maps.Keys(plan.Status.ClusterSchedulingStatuses),
	) {
		status := plan.Status.ClusterSchedulingStatuses[cluster]
		rows = append(rows, []string{
			plan.Metadata.Name + "/" + cluster,
			scalar(status.Version),
			"<none>",
			scalar(status.State),
			scalar(status.TargetNamespace),
			scalar(status.JobName),
		})
	}
	return rows
}

func hostStatusRow(plan kubesphereextension.InstallPlan) []string {
	version := plan.Status.Version
	if version == "" {
		version = plan.Spec.Extension.Version
	}
	return []string{
		scalar(plan.Metadata.Name),
		scalar(version),
		installPlanEnabled(plan),
		scalar(plan.Status.State),
		scalar(plan.Status.TargetNamespace),
		scalar(plan.Status.JobName),
	}
}

func installPlanEnabled(plan kubesphereextension.InstallPlan) string {
	if plan.Status.Enabled != nil {
		return optionalBool(plan.Status.Enabled)
	}
	return strconv.FormatBool(plan.Spec.Enabled)
}

func printWatchHeader(out io.Writer) error {
	_, err := fmt.Fprintf(out, "%-11s%-13s%s\n", "STATE", "NAMESPACE", "JOB")
	return err
}

func printWatchRow(
	out io.Writer,
	event kubesphereextension.StateEvent,
) error {
	_, err := fmt.Fprintf(
		out,
		"%-11s%-13s%s\n",
		pendingScalar(event.State),
		scalar(event.TargetNamespace),
		scalar(event.JobName),
	)
	return err
}

func writeTable(out io.Writer, rows [][]string) error {
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		for index, value := range row {
			if index != 0 {
				if _, err := io.WriteString(writer, "\t"); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(writer, tableCell(value)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, "\n"); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func tableCell(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = printers.EscapeTerminal(value)
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
}
