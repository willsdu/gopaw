package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/willisdu/gopaw/internal/version"
)

func newVersionCmd(opts rootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			v := version.Version
			if version.Commit != "" {
				v += " (" + version.Commit + ")"
			}
			if version.Date != "" {
				fmt.Fprintf(out, "%s %s\n", v, version.Date)
				return nil
			}
			fmt.Fprintln(out, v)
			return nil
		},
	}
	cmd.SetOut(opts.Out)
	cmd.SetErr(opts.Err)
	return cmd
}
