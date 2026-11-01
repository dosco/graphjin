package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
)

func newResetCmd() *cobra.Command {
	var keepSecrets bool
	var all bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Tear down the stack and clear local state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sup := compose.New(assets.ComposeFS)
			files, err := sup.Materialize(compose.ModeDemo)
			if err != nil {
				return err
			}
			// Tear down every possible project name an operator could have used
			// for this instance, so reset works regardless of the last mode without
			// touching another customer's projects.
			for _, m := range []compose.Mode{compose.ModeProd, compose.ModeDev, compose.ModeDemo} {
				_, _ = sup.Run(context.Background(), compose.ProjectNameForMode(m), files, []string{"down", "-v"}, os.Stdout, os.Stderr)
			}
			project, err := sup.ProjectName("")
			if err != nil {
				return err
			}

			cfgFile := filepath.Join(config.Dir(""), "config.json")
			if err := removeIfExists(cfgFile); err != nil {
				return err
			}
			if all {
				// marketplaces.json and plugins.json are left over from the
				// removed plugin system; clear them too on a full reset.
				for _, name := range []string{"secrets.json", "marketplaces.json", "plugins.json", "compose.override.yml"} {
					if err := removeIfExists(filepath.Join(config.Dir(""), name)); err != nil {
						return err
					}
				}
			} else if !keepSecrets {
				// Default: keep secrets; only config.json is wiped.
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reset complete; %s\n", backupsKeptNote(sup, project))
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepSecrets, "keep-secrets", true, "Preserve ~/.config/openneko/secrets.json")
	cmd.Flags().BoolVar(&all, "all", false, "Also remove secrets and the compose override")
	return cmd
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
