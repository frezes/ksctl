package extension

import (
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

type ChangeMode uint8

const (
	Keep ChangeMode = iota
	Replace
	Clear
)

type StringChange struct {
	Mode  ChangeMode
	Value string
}

type SchedulingChange struct {
	Mode            ChangeMode
	Clusters        []string
	SetOverrides    map[string]string
	RemoveOverrides []string
}

type PlanChanges struct {
	Config     StringChange
	Scheduling SchedulingChange
}

type ChangeSummary struct {
	ConfigChanged     bool
	SchedulingChanged bool
	ResultScheduling  *ClusterScheduling
}

func (s ChangeSummary) Changed() bool {
	return s.ConfigChanged || s.SchedulingChanged
}

func NormalizeYAML(kind, value string) (string, error) {
	normalized := strings.TrimRight(value, "\r\n")
	if strings.TrimSpace(normalized) == "" {
		return "", fmt.Errorf("%s must be non-empty YAML", kind)
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(normalized), 4096)
	var first any
	if err := decoder.Decode(&first); err != nil {
		return "", fmt.Errorf("invalid %s: %w", kind, err)
	}
	var second any
	if err := decoder.Decode(&second); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("%s must contain exactly one YAML document", kind)
		}
		return "", fmt.Errorf("invalid %s: %w", kind, err)
	}
	return normalized + "\n", nil
}

func NormalizeClusters(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("cluster name must be non-empty")
		}
		if err := validatePathName("cluster", name); err != nil {
			return nil, err
		}
		if _, found := seen[name]; found {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func BuildInstallScheduling(
	installationMode string,
	clusters []string,
	overrides map[string]string,
) (*ClusterScheduling, error) {
	normalizedClusters, err := NormalizeClusters(clusters)
	if err != nil {
		return nil, err
	}
	if len(normalizedClusters) == 0 && len(overrides) == 0 {
		return nil, nil
	}
	if installationMode != "Multicluster" {
		return nil, fmt.Errorf(
			"extension version uses %s installation mode and does not accept cluster scheduling",
			valueOrNone(installationMode),
		)
	}

	placed := make(map[string]struct{}, len(normalizedClusters))
	for _, cluster := range normalizedClusters {
		placed[cluster] = struct{}{}
	}
	normalizedOverrides := make(map[string]string, len(overrides))
	for _, cluster := range slices.Sorted(maps.Keys(overrides)) {
		if err := validatePathName("cluster", cluster); err != nil {
			return nil, err
		}
		if _, found := placed[cluster]; !found {
			return nil, fmt.Errorf(
				"override cluster %q is not present in --clusters",
				cluster,
			)
		}
		value, err := NormalizeYAML(
			"override for cluster "+strconv.Quote(cluster),
			overrides[cluster],
		)
		if err != nil {
			return nil, err
		}
		normalizedOverrides[cluster] = value
	}
	return &ClusterScheduling{
		Placement: &Placement{Clusters: normalizedClusters},
		Overrides: normalizedOverrides,
	}, nil
}

func cloneScheduling(value *ClusterScheduling) *ClusterScheduling {
	if value == nil {
		return nil
	}
	copy := *value
	if value.Placement != nil {
		placement := *value.Placement
		placement.Clusters = slices.Clone(value.Placement.Clusters)
		copy.Placement = &placement
	}
	copy.Overrides = maps.Clone(value.Overrides)
	return &copy
}

func BuildSpecPatch(
	current InstallPlan,
	installationMode string,
	changes PlanChanges,
) (map[string]any, ChangeSummary, error) {
	specPatch := map[string]any{"upgradeStrategy": "Manual"}
	summary := ChangeSummary{}

	switch changes.Config.Mode {
	case Keep:
	case Replace:
		normalized, err := NormalizeYAML(
			"global configuration",
			changes.Config.Value,
		)
		if err != nil {
			return nil, ChangeSummary{}, err
		}
		if normalized != current.Spec.Config {
			specPatch["config"] = normalized
			summary.ConfigChanged = true
		}
	case Clear:
		if current.Spec.Config != "" {
			specPatch["config"] = nil
			summary.ConfigChanged = true
		}
	default:
		return nil, ChangeSummary{}, fmt.Errorf(
			"invalid configuration change mode %d",
			changes.Config.Mode,
		)
	}

	result := cloneScheduling(current.Spec.ClusterScheduling)
	schedulingPatch := map[string]any{}
	overridePatch := map[string]any{}

	removeSet := make(map[string]struct{}, len(changes.Scheduling.RemoveOverrides))
	for _, cluster := range changes.Scheduling.RemoveOverrides {
		if err := validatePathName("cluster", cluster); err != nil {
			return nil, ChangeSummary{}, err
		}
		if _, found := changes.Scheduling.SetOverrides[cluster]; found {
			return nil, ChangeSummary{}, fmt.Errorf(
				"cluster %q cannot be both set and removed as an override",
				cluster,
			)
		}
		removeSet[cluster] = struct{}{}
	}

	if changes.Scheduling.Mode == Clear &&
		(len(changes.Scheduling.SetOverrides) != 0 || len(removeSet) != 0) {
		return nil, ChangeSummary{}, fmt.Errorf(
			"clear cluster scheduling cannot be combined with override changes",
		)
	}

	switch changes.Scheduling.Mode {
	case Keep:
	case Replace:
		clusters, err := NormalizeClusters(changes.Scheduling.Clusters)
		if err != nil {
			return nil, ChangeSummary{}, err
		}
		if result == nil {
			result = &ClusterScheduling{}
		}
		placed := make(map[string]struct{}, len(clusters))
		for _, cluster := range clusters {
			placed[cluster] = struct{}{}
		}
		for _, cluster := range slices.Sorted(maps.Keys(result.Overrides)) {
			if _, found := placed[cluster]; !found {
				delete(result.Overrides, cluster)
				overridePatch[cluster] = nil
			}
		}
		result.Placement = &Placement{Clusters: clusters}
		schedulingPatch["placement"] = map[string]any{
			"clusters":        clusters,
			"clusterSelector": nil,
		}
	case Clear:
		result = nil
	default:
		return nil, ChangeSummary{}, fmt.Errorf(
			"invalid scheduling change mode %d",
			changes.Scheduling.Mode,
		)
	}

	setNames := slices.Sorted(maps.Keys(changes.Scheduling.SetOverrides))
	if len(setNames) != 0 {
		if result == nil || result.Placement == nil {
			return nil, ChangeSummary{}, fmt.Errorf(
				"cannot set overrides without cluster placement",
			)
		}
		if result.Placement.ClusterSelector != nil &&
			changes.Scheduling.Mode != Replace {
			return nil, ChangeSummary{}, fmt.Errorf(
				"setting an override with selector-only placement requires --clusters",
			)
		}
		placed := make(map[string]struct{}, len(result.Placement.Clusters))
		for _, cluster := range result.Placement.Clusters {
			placed[cluster] = struct{}{}
		}
		if result.Overrides == nil {
			result.Overrides = map[string]string{}
		}
		for _, cluster := range setNames {
			if err := validatePathName("cluster", cluster); err != nil {
				return nil, ChangeSummary{}, err
			}
			if _, found := placed[cluster]; !found {
				return nil, ChangeSummary{}, fmt.Errorf(
					"override cluster %q is not in the resulting placement",
					cluster,
				)
			}
			value, err := NormalizeYAML(
				"override for cluster "+strconv.Quote(cluster),
				changes.Scheduling.SetOverrides[cluster],
			)
			if err != nil {
				return nil, ChangeSummary{}, err
			}
			if result.Overrides[cluster] != value {
				result.Overrides[cluster] = value
				overridePatch[cluster] = value
			}
		}
	}

	for _, cluster := range slices.Sorted(maps.Keys(removeSet)) {
		if result == nil {
			continue
		}
		if _, found := result.Overrides[cluster]; found {
			delete(result.Overrides, cluster)
			overridePatch[cluster] = nil
		}
	}

	if installationMode != "Multicluster" && result != nil {
		return nil, ChangeSummary{}, fmt.Errorf(
			"extension version uses %s installation mode and does not accept cluster scheduling",
			valueOrNone(installationMode),
		)
	}

	summary.SchedulingChanged = !reflect.DeepEqual(
		current.Spec.ClusterScheduling,
		result,
	)
	summary.ResultScheduling = result
	if summary.SchedulingChanged {
		if result == nil {
			specPatch["clusterScheduling"] = nil
		} else {
			if len(overridePatch) != 0 {
				schedulingPatch["overrides"] = overridePatch
			}
			specPatch["clusterScheduling"] = schedulingPatch
		}
	}
	return specPatch, summary, nil
}
