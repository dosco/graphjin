package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/version"
)

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the openneko version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), version.Version)
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), version.Detailed())
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "Print only the version number")
	return cmd
}
