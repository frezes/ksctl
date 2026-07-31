package extension

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	kubesphereextension "github.com/kubesphere/ksctl/pkg/kubesphere/extension"
	"github.com/spf13/cobra"
)

const maxConfigurationInputBytes = 10 << 20

type configurationFlags struct {
	configFile             string
	clusters               []string
	overrides              []string
	removeOverrides        []string
	clearConfig            bool
	clearClusterScheduling bool
}

type loadedConfiguration struct {
	Config          *string
	Clusters        []string
	Overrides       map[string]string
	RemoveOverrides []string
}

type overrideInput struct {
	cluster string
	path    string
}

func readInput(path string, stdin io.Reader) (string, error) {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = stdin
	} else {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open configuration input %q: %w", path, err)
		}
		defer file.Close()
		reader = file
	}

	data, err := io.ReadAll(io.LimitReader(
		reader,
		maxConfigurationInputBytes+1,
	))
	if err != nil {
		return "", fmt.Errorf("read configuration input %q: %w", path, err)
	}
	if len(data) > maxConfigurationInputBytes {
		return "", fmt.Errorf(
			"configuration input %q exceeds %d bytes",
			path,
			maxConfigurationInputBytes,
		)
	}
	return string(data), nil
}

func (flags *configurationFlags) load(
	command *cobra.Command,
	stdin io.Reader,
) (loadedConfiguration, error) {
	parsedOverrides := make([]overrideInput, 0, len(flags.overrides))
	overrideNames := make(map[string]struct{}, len(flags.overrides))
	stdinConsumers := 0
	if command.Flags().Changed("config") && flags.configFile == "-" {
		stdinConsumers++
	}
	for _, value := range flags.overrides {
		cluster, path, found := strings.Cut(value, "=")
		if !found || cluster == "" || path == "" {
			return loadedConfiguration{}, fmt.Errorf(
				"--override must use CLUSTER=FILE",
			)
		}
		if err := validateCommandPathName("override cluster", cluster); err != nil {
			return loadedConfiguration{}, err
		}
		if _, duplicate := overrideNames[cluster]; duplicate {
			return loadedConfiguration{}, fmt.Errorf(
				"override for cluster %q was provided more than once",
				cluster,
			)
		}
		overrideNames[cluster] = struct{}{}
		parsedOverrides = append(parsedOverrides, overrideInput{
			cluster: cluster,
			path:    path,
		})
		if path == "-" {
			stdinConsumers++
		}
	}
	for _, cluster := range flags.clusters {
		if err := validateCommandPathName(
			"cluster",
			strings.TrimSpace(cluster),
		); err != nil {
			return loadedConfiguration{}, err
		}
	}

	removeNames := make(map[string]struct{}, len(flags.removeOverrides))
	removeOverrides := make([]string, 0, len(flags.removeOverrides))
	for _, cluster := range flags.removeOverrides {
		if err := validateCommandPathName("override cluster", cluster); err != nil {
			return loadedConfiguration{}, err
		}
		if _, duplicate := removeNames[cluster]; duplicate {
			continue
		}
		if _, set := overrideNames[cluster]; set {
			return loadedConfiguration{}, fmt.Errorf(
				"cluster %q cannot be both set and removed as an override",
				cluster,
			)
		}
		removeNames[cluster] = struct{}{}
		removeOverrides = append(removeOverrides, cluster)
	}
	if stdinConsumers > 1 {
		return loadedConfiguration{}, fmt.Errorf(
			"at most one configuration input may read from stdin",
		)
	}

	loaded := loadedConfiguration{
		Clusters:        slices.Clone(flags.clusters),
		Overrides:       make(map[string]string, len(parsedOverrides)),
		RemoveOverrides: removeOverrides,
	}
	if command.Flags().Changed("config") {
		value, err := readInput(flags.configFile, stdin)
		if err != nil {
			return loadedConfiguration{}, err
		}
		loaded.Config = &value
	}
	for _, override := range parsedOverrides {
		value, err := readInput(override.path, stdin)
		if err != nil {
			return loadedConfiguration{}, fmt.Errorf(
				"read override for cluster %q: %w",
				override.cluster,
				err,
			)
		}
		loaded.Overrides[override.cluster] = value
	}
	return loaded, nil
}

func (flags *configurationFlags) planChanges(
	command *cobra.Command,
	loaded loadedConfiguration,
) kubesphereextension.PlanChanges {
	changes := kubesphereextension.PlanChanges{}
	switch {
	case flags.clearConfig:
		changes.Config.Mode = kubesphereextension.Clear
	case command.Flags().Changed("config"):
		changes.Config = kubesphereextension.StringChange{
			Mode:  kubesphereextension.Replace,
			Value: *loaded.Config,
		}
	}
	switch {
	case flags.clearClusterScheduling:
		changes.Scheduling.Mode = kubesphereextension.Clear
	case command.Flags().Changed("clusters"):
		changes.Scheduling.Mode = kubesphereextension.Replace
		changes.Scheduling.Clusters = slices.Clone(loaded.Clusters)
	}
	changes.Scheduling.SetOverrides = loaded.Overrides
	changes.Scheduling.RemoveOverrides = slices.Clone(
		loaded.RemoveOverrides,
	)
	return changes
}

func (flags *configurationFlags) hasChanges(command *cobra.Command) bool {
	for _, name := range []string{
		"config",
		"clusters",
		"override",
		"remove-override",
	} {
		if command.Flags().Changed(name) {
			return true
		}
	}
	return flags.clearConfig || flags.clearClusterScheduling
}
