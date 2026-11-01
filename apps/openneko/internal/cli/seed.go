package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

func newSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed [adventureworks]",
		Short: "Load demo data (currently only adventureworks)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "adventureworks"
			if len(args) == 1 {
				target = args[0]
			}
			if target != "adventureworks" {
				return WithExit(2, fmt.Errorf("seed: unknown target %q (want: adventureworks)", target))
			}
			if instance.Current() != "" {
				settings, ok, err := requireConfiguredNamedInstallation()
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("instance %q has not been configured; run `openneko --instance %s setup --mode demo` first", instance.Current(), instance.Current())
				}
				if settings.Mode != string(compose.ModeDemo) {
					return fmt.Errorf("instance %q is installed in %s mode; AdventureWorks seed requires a demo instance", instance.Current(), settings.Mode)
				}
			}
			sup := compose.New(assets.ComposeFS)
			project, err := sup.ProjectName(compose.ModeDemo)
			if err != nil {
				return err
			}
			files, err := sup.Materialize(compose.ModeDemo)
			if err != nil {
				return err
			}
			// Seed always lives under the demo project, regardless of what
			// other mode the operator may be running in this dir.
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// One-shot: load the AdventureWorks CSVs.
			if code, err := sup.Run(ctx, project, files, []string{"run", "--rm", "adventureworks-init"}, os.Stdout, os.Stderr); err != nil {
				return err
			} else if code != 0 {
				return WithExit(code, errors.New("adventureworks-init failed"))
			}
			// One-shot: seed neko-db workflows that reference adventureworks data.
			if code, err := sup.Run(ctx, project, files, []string{"run", "--rm", "neko-adventureworks-seed"}, os.Stdout, os.Stderr); err != nil {
				return err
			} else if code != 0 {
				return WithExit(code, errors.New("neko-adventureworks-seed failed"))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "demo seed complete — bring the simulators up with `openneko start --mode demo`")
			return nil
		},
	}
}
