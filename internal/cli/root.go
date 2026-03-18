package cli

import (
	"io"

	"github.com/spf13/cobra"
)

type rootOpts struct {
	Out io.Writer
	Err io.Writer
}

type rootFlags struct {
	configPath string
}

func newRootCmd(opts rootOpts) *cobra.Command {
	flags := &rootFlags{}

	cmd := &cobra.Command{
		Use:           "gopaw",
		Short:         "copaw-like Go project skeleton",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetOut(opts.Out)
	cmd.SetErr(opts.Err)

	cmd.PersistentFlags().StringVar(&flags.configPath, "config", "", "config file path (json)")

	cmd.AddCommand(newVersionCmd(opts))
	cmd.AddCommand(newProvidersCmd(opts, flags))
	cmd.AddCommand(newAppCmd(opts, flags))

	return cmd
}

