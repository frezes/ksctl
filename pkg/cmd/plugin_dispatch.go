package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var pluginFilenamePrefixes = []string{"ksctl"}

func NewRootCommandWithArgs(streams IOStreams, info VersionInfo, arguments []string) (*cobra.Command, error) {
	return newRootCommandWithArgs(
		"ksctl",
		"",
		streams,
		info,
		arguments,
		newDefaultPluginHandler(pluginFilenamePrefixes),
	)
}

func NewKubectlPluginCommandWithArgs(streams IOStreams, info VersionInfo, arguments []string) (*cobra.Command, error) {
	return newRootCommandWithArgs(
		"kubectl-ks",
		"kubectl ks",
		streams,
		info,
		arguments,
		newDefaultPluginHandler(pluginFilenamePrefixes),
	)
}

func newRootCommandWithArgs(
	use, displayName string,
	streams IOStreams,
	info VersionInfo,
	arguments []string,
	handler pluginHandler,
) (*cobra.Command, error) {
	root := newRootCommand(use, displayName, streams, info)
	if err := dispatchPlugin(root, arguments, handler); err != nil {
		return nil, err
	}
	return root, nil
}

func dispatchPlugin(root *cobra.Command, arguments []string, handler pluginHandler) error {
	if handler == nil || len(arguments) <= 1 {
		return nil
	}

	commandPath := arguments[1:]
	found, _, findErr := root.Find(commandPath)
	if findErr == nil || found != root {
		return nil
	}

	var commandName string
	for _, argument := range commandPath {
		if !strings.HasPrefix(argument, "-") {
			commandName = argument
			break
		}
	}
	switch commandName {
	case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return nil
	}

	return handlePluginCommand(handler, commandPath, 1)
}

type pluginHandler interface {
	Lookup(filename string) (string, bool)
	Execute(executablePath string, arguments, environment []string) error
}

// defaultPluginHandler follows kubectl v0.36.2's executable plugin behavior
// without importing kubectl's complete top-level command package.
type defaultPluginHandler struct {
	validPrefixes []string
}

func newDefaultPluginHandler(validPrefixes []string) *defaultPluginHandler {
	return &defaultPluginHandler{validPrefixes: validPrefixes}
}

func (h *defaultPluginHandler) Lookup(filename string) (string, bool) {
	for _, prefix := range h.validPrefixes {
		path, err := exec.LookPath(fmt.Sprintf("%s-%s", prefix, filename))
		if (err != nil && !errors.Is(err, exec.ErrDot)) || path == "" {
			continue
		}
		return path, true
	}
	return "", false
}

func (h *defaultPluginHandler) Execute(executablePath string, arguments, environment []string) error {
	if runtime.GOOS == "windows" {
		command := pluginCommand(executablePath, arguments...)
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		command.Env = environment
		if err := command.Run(); err != nil {
			return err
		}
		os.Exit(0)
	}
	return syscall.Exec(executablePath, append([]string{executablePath}, arguments...), environment)
}

func pluginCommand(name string, arguments ...string) *exec.Cmd {
	command := &exec.Cmd{
		Path: name,
		Args: append([]string{name}, arguments...),
	}
	if filepath.Base(name) == name {
		path, err := exec.LookPath(name)
		if path != "" && (err == nil || errors.Is(err, exec.ErrDot)) {
			command.Path = path
		}
	}
	return command
}

func handlePluginCommand(handler pluginHandler, arguments []string, minimumNameWords int) error {
	var remaining []string
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "-") {
			break
		}
		remaining = append(remaining, strings.ReplaceAll(argument, "-", "_"))
	}
	if len(remaining) == 0 {
		return fmt.Errorf("flags cannot be placed before plugin name: %s", arguments[0])
	}

	var executablePath string
	for len(remaining) > 0 {
		path, found := handler.Lookup(strings.Join(remaining, "-"))
		if found {
			executablePath = path
			break
		}
		remaining = remaining[:len(remaining)-1]
		if len(remaining) < minimumNameWords {
			break
		}
	}
	if executablePath == "" {
		return nil
	}
	return handler.Execute(executablePath, arguments[len(remaining):], os.Environ())
}
