package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
)

func newStopCmd() *cobra.Command {
	var volumes bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Bring down the OpenNeko stack",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sup := compose.New(assets.ComposeFS)
			// Materialize against the broadest mode (demo) so we tear down any
			// overlay services the operator may have started.
			files, err := sup.Materialize(compose.ModeDemo)
			if err != nil {
				return err
			}
			// Reads the project name persisted by the matching `openneko start`.
			project, err := sup.ProjectName("")
			if err != nil {
				return err
			}
			args := []string{"down"}
			if volumes {
				args = append(args, "-v")
			}
			code, err := sup.Run(context.Background(), project, files, args, os.Stdout, os.Stderr)
			if err != nil {
				return err
			}
			if code != 0 {
				return WithExit(code, nil)
			}
			if volumes {
				fmt.Fprintf(cmd.OutOrStdout(), "data volumes removed; %s\n", backupsKeptNote(sup, project))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&volumes, "volumes", "v", false, "Also remove named volumes (deletes data)")
	return cmd
}
