package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/installation"
)

type installationInventoryEntry struct {
	Name     string
	Settings installation.Settings
}

func newInstancesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "instances",
		Short: "List configured OpenNeko installations on this host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			entries, err := configuredInstallations()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No configured OpenNeko installations found.")
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "INSTANCE\tMODE\tWEB\tCOMPOSE PROJECT")
			for _, entry := range entries {
				label := entry.Name
				if label == "" {
					label = "(default)"
				}
				settings := entry.Settings
				webHost := settings.WebBindAddress
				if webHost == "0.0.0.0" {
					webHost = "localhost"
				}
				fmt.Fprintf(
					writer,
					"%s\t%s\thttp://%s:%d\t%s\n",
					label,
					settings.Mode,
					webHost,
					settings.WebPort,
					compose.ProjectNameForModeAndInstance(compose.Mode(settings.Mode), settings.Instance),
				)
			}
			return writer.Flush()
		},
	}
}

func configuredInstallations() ([]installationInventoryEntry, error) {
	root := config.RootDir()
	var entries []installationInventoryEntry
	if settings, ok, err := installation.Load(root); err != nil {
		return nil, err
	} else if ok {
		if settings.Instance != "" {
			return nil, fmt.Errorf("default installation settings contain named instance %q", settings.Instance)
		}
		entries = append(entries, installationInventoryEntry{Settings: settings})
	}

	instancesDir := filepath.Join(root, "instances")
	dirs, err := os.ReadDir(instancesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		settings, ok, err := installation.Load(filepath.Join(instancesDir, dir.Name()))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if settings.Instance != dir.Name() {
			return nil, fmt.Errorf("installation directory %q contains settings for instance %q", dir.Name(), settings.Instance)
		}
		entries = append(entries, installationInventoryEntry{Name: dir.Name(), Settings: settings})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
