package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

const filenamePrefix = "ksctl"

func NewCommand(parent string, streams genericiooptions.IOStreams) *cobra.Command {
	example := fmt.Sprintf("  # List all available plugins\n  %s plugin list\n\n  # List only plugin executable names\n  %s plugin list --name-only", parent, parent)
	command := &cobra.Command{
		Use:     "plugin [flags]",
		Short:   "Provides utilities for interacting with plugins",
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newListCommand(example, streams))
	return command
}

type pathVerifier interface {
	verify(path string) []error
}

type listOptions struct {
	verifier    pathVerifier
	nameOnly    bool
	pluginPaths []string
	genericiooptions.IOStreams
}

func newListCommand(example string, streams genericiooptions.IOStreams) *cobra.Command {
	o := &listOptions{IOStreams: streams}
	command := &cobra.Command{
		Use:     "list",
		Short:   "List all visible plugin executables on a user's PATH",
		Long:    "List all ksctl plugin files on PATH. Candidates begin with ksctl- and are checked for executability, shadowing, and built-in command conflicts.",
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			o.complete(command)
			return o.run()
		},
	}
	command.Flags().BoolVar(&o.nameOnly, "name-only", false, "Display only plugin executable names instead of full paths")
	return command
}

func (o *listOptions) complete(command *cobra.Command) {
	o.verifier = &commandOverrideVerifier{
		root:        command.Root(),
		seenPlugins: map[string]string{},
	}
	o.pluginPaths = filepath.SplitList(os.Getenv("PATH"))
}

func (o *listOptions) run() error {
	plugins, pluginErrors := o.listPlugins()
	if len(plugins) > 0 {
		fmt.Fprintln(o.Out, "The following ksctl-compatible plugins are available:")
		fmt.Fprintln(o.Out)
	} else {
		pluginErrors = append(pluginErrors, errors.New("error: unable to find any ksctl plugins in your PATH"))
	}

	warningCount := 0
	for _, pluginPath := range plugins {
		if o.nameOnly {
			fmt.Fprintln(o.Out, filepath.Base(pluginPath))
		} else {
			fmt.Fprintln(o.Out, pluginPath)
		}
		for _, warning := range o.verifier.verify(pluginPath) {
			fmt.Fprintf(o.ErrOut, "  - %s\n", warning)
			warningCount++
		}
	}

	if warningCount == 1 {
		pluginErrors = append(pluginErrors, errors.New("error: one plugin warning was found"))
	} else if warningCount > 1 {
		pluginErrors = append(pluginErrors, fmt.Errorf("error: %d plugin warnings were found", warningCount))
	}
	if len(pluginErrors) == 0 {
		return nil
	}

	var combined bytes.Buffer
	for _, pluginError := range pluginErrors {
		fmt.Fprintln(&combined, pluginError)
	}
	return errors.New(combined.String())
}

func (o *listOptions) listPlugins() ([]string, []error) {
	var plugins []string
	var pluginErrors []error
	for _, directory := range uniquePaths(o.pluginPaths) {
		if strings.TrimSpace(directory) == "" {
			continue
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			var pathError *os.PathError
			if errors.As(err, &pathError) {
				fmt.Fprintf(o.ErrOut, "Unable to read directory %q from your PATH: %v. Skipping...\n", directory, err)
				continue
			}
			pluginErrors = append(pluginErrors, fmt.Errorf("error: unable to read directory %q in your PATH: %w", directory, err))
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), filenamePrefix+"-") {
				continue
			}
			plugins = append(plugins, filepath.Join(directory, entry.Name()))
		}
	}
	return plugins, pluginErrors
}

type commandOverrideVerifier struct {
	root        *cobra.Command
	seenPlugins map[string]string
}

func (v *commandOverrideVerifier) verify(path string) []error {
	if v.root == nil {
		return []error{errors.New("unable to verify path with nil root command")}
	}
	basename := filepath.Base(path)
	commandPath := strings.Split(strings.TrimPrefix(basename, filenamePrefix+"-"), "-")
	var verificationErrors []error

	executable, err := isExecutable(path)
	if err != nil {
		verificationErrors = append(verificationErrors, fmt.Errorf("error: unable to identify %s as an executable file: %w", path, err))
	} else if !executable {
		verificationErrors = append(verificationErrors, fmt.Errorf("warning: %s is identified as a ksctl plugin, but is not executable", path))
	}

	if existing, ok := v.seenPlugins[basename]; ok {
		verificationErrors = append(verificationErrors, fmt.Errorf("warning: %s is shadowed by a similarly named plugin: %s", path, existing))
	} else {
		v.seenPlugins[basename] = path
	}

	if command, _, findErr := v.root.Find(commandPath); findErr == nil && command != v.root {
		verificationErrors = append(verificationErrors, fmt.Errorf("warning: %s overwrites existing command: %q", basename, command.CommandPath()))
	}
	return verificationErrors
}

func isExecutable(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".bat", ".cmd", ".com", ".exe", ".ps1":
			return true, nil
		default:
			return false, nil
		}
	}
	return !info.IsDir() && info.Mode()&0o111 != 0, nil
}

func uniquePaths(paths []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}
