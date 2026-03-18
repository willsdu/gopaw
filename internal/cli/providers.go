package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newProvidersCmd(opts rootOpts, rootFlags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "List available providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "providers:")
			fmt.Fprintln(out, "- openai")
			fmt.Fprintln(out, "- anthropic")
			fmt.Fprintln(out, "- ollama")
			fmt.Fprintln(out, "- minimax")
			if rootFlags.configPath != "" {
				fmt.Fprintf(out, "\nconfig: %s\n", rootFlags.configPath)
			}
			return nil
		},
	}
	cmd.SetOut(opts.Out)
	cmd.SetErr(opts.Err)
	return cmd
}

