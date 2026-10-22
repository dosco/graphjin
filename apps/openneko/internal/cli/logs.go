package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs [service...]",
		Short: "Stream logs from one or more services",
		RunE: func(cmd *cobra.Command, args []string) error {
			sup := compose.New(assets.ComposeFS)
			files, err := sup.Materialize(compose.ModeDemo)
			if err != nil {
				return err
			}
			project, err := sup.ProjectName("")
			if err != nil {
				return err
			}
			dargs := []string{"logs"}
			if follow {
				dargs = append(dargs, "-f")
			}
			dargs = append(dargs, args...)
			code, err := sup.Run(context.Background(), project, files, dargs, os.Stdout, os.Stderr)
			if err != nil {
				return err
			}
			if code != 0 {
				return WithExit(code, nil)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow new log lines")
	return cmd
}
