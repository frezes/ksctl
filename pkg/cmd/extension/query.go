package extension

import (
	"fmt"
	"strings"
	"time"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func newListCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var category string
	var installedOnly bool
	var output string
	command := &cobra.Command{
		Use:     "list",
		Short:   "List available KubeSphere extensions",
		Example: parent + " extension list --installed",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			format, err := parseOutput(output, true)
			if err != nil {
				return err
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			result, err := service.List(
				command.Context(),
				internalextension.ListOptions{
					Category:      category,
					InstalledOnly: installedOnly,
				},
			)
			if err != nil {
				return err
			}
			if format == outputJSON || format == outputYAML {
				err = writeStructured(streams.Out, result, format)
			} else {
				err = printList(streams.Out, result, format)
			}
			if err != nil {
				return fmt.Errorf("write extension list output: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(
		&category,
		"category",
		"",
		"Filter by extension category",
	)
	command.Flags().BoolVar(
		&installedOnly,
		"installed",
		false,
		"Show only extensions with active InstallPlans",
	)
	command.Flags().StringVarP(
		&output,
		"output",
		"o",
		string(outputTable),
		"Output format: table, wide, json, or yaml",
	)
	return command
}

func newShowCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var version string
	var output string
	command := &cobra.Command{
		Use:     "show NAME",
		Short:   "Show KubeSphere extension details",
		Example: parent + " extension show NAME [--version VERSION]",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			format, err := parseOutput(output, true)
			if err != nil {
				return err
			}
			if command.Flags().Changed("version") &&
				strings.TrimSpace(version) == "" {
				return fmt.Errorf("--version must be non-empty")
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			result, err := service.Show(command.Context(), args[0], version)
			if err != nil {
				return err
			}
			if format == outputJSON || format == outputYAML {
				err = writeStructured(streams.Out, result, format)
			} else {
				err = printShow(streams.Out, result, format)
			}
			if err != nil {
				return fmt.Errorf("write extension show output: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(
		&version,
		"version",
		"",
		"Show one exact extension version",
	)
	command.Flags().StringVarP(
		&output,
		"output",
		"o",
		string(outputTable),
		"Output format: table, wide, json, or yaml",
	)
	return command
}

func newVersionsCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:     "versions NAME",
		Short:   "List versions of a KubeSphere extension",
		Example: parent + " extension versions NAME",
		Args:    exactExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			format, err := parseOutput(output, false)
			if err != nil {
				return err
			}
			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			result, err := service.Versions(command.Context(), args[0])
			if err != nil {
				return err
			}
			if format == outputJSON || format == outputYAML {
				err = writeStructured(streams.Out, result, format)
			} else {
				err = printVersions(streams.Out, result)
			}
			if err != nil {
				return fmt.Errorf("write extension versions output: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVarP(
		&output,
		"output",
		"o",
		string(outputTable),
		"Output format: table, json, or yaml",
	)
	return command
}

func newStatusCommand(
	parent string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) *cobra.Command {
	var watch bool
	var waitTimeout time.Duration
	var output string
	command := &cobra.Command{
		Use:     "status [NAME]",
		Short:   "Show KubeSphere extension installation status",
		Example: parent + " extension status [NAME] [--watch]",
		Args:    optionalExtensionNameArgs,
		RunE: func(command *cobra.Command, args []string) error {
			format, err := parseOutput(output, false)
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			if watch && name == "" {
				return fmt.Errorf("--watch requires NAME")
			}
			if watch && format != outputTable {
				return fmt.Errorf("--watch supports table output only")
			}
			if command.Flags().Changed("wait-timeout") && !watch {
				return fmt.Errorf("--wait-timeout requires --watch")
			}
			if watch && waitTimeout <= 0 {
				return fmt.Errorf("watch timeout must be positive")
			}

			service, err := serviceAfterValidation(factory)
			if err != nil {
				return err
			}
			if watch {
				headerWritten := false
				_, err := service.Watch(
					command.Context(),
					name,
					internalextension.PollOptions{
						Timeout: waitTimeout,
						OnState: func(event internalextension.StateEvent) error {
							if !headerWritten {
								if err := printWatchHeader(streams.Out); err != nil {
									return fmt.Errorf(
										"write extension status watch output: %w",
										err,
									)
								}
								headerWritten = true
							}
							if err := printWatchRow(streams.Out, event); err != nil {
								return fmt.Errorf(
									"write extension status watch output: %w",
									err,
								)
							}
							return nil
						},
					},
				)
				return err
			}

			result, err := service.Status(command.Context(), name)
			if err != nil {
				return err
			}
			if format == outputJSON || format == outputYAML {
				err = writeStructured(streams.Out, result, format)
			} else {
				err = printStatus(streams.Out, result)
			}
			if err != nil {
				return fmt.Errorf("write extension status output: %w", err)
			}
			return nil
		},
	}
	command.Flags().BoolVar(
		&watch,
		"watch",
		false,
		"Watch distinct host installation state changes",
	)
	command.Flags().DurationVar(
		&waitTimeout,
		"wait-timeout",
		10*time.Minute,
		"Maximum time to watch lifecycle state",
	)
	command.Flags().StringVarP(
		&output,
		"output",
		"o",
		string(outputTable),
		"Output format: table, json, or yaml",
	)
	return command
}
