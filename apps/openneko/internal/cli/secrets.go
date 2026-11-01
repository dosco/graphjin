package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/prompt"
	"github.com/open-neko/neko/apps/openneko/internal/secrets"
)

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage deployment secrets (channel, sign-in and pack credentials) stored at ~/.config/openneko/secrets.json",
	}
	cmd.AddCommand(newSecretsListCmd(), newSecretsSetCmd(), newSecretsUnsetCmd())
	return cmd
}

func newSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [<section>]",
		Short: "Show env keys stored for a section (or all sections); values never echoed",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			store, err := secrets.Read("")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(args) == 1 {
				section := args[0]
				keys := secrets.ListKeysForSection(store, section)
				if len(keys) == 0 {
					fmt.Fprintln(out, "no secrets stored")
					return nil
				}
				fmt.Fprintln(out, section)
				for _, k := range keys {
					fmt.Fprintf(out, "  %s\n", k)
				}
				return nil
			}
			if len(store) == 0 {
				fmt.Fprintln(out, "no secrets stored")
				return nil
			}
			sections := sortedSectionNames(store)
			for _, p := range sections {
				keys := secrets.ListKeysForSection(store, p)
				if len(keys) == 0 {
					continue
				}
				fmt.Fprintln(out, p)
				for _, k := range keys {
					fmt.Fprintf(out, "  %s\n", k)
				}
			}
			return nil
		},
	}
}

func sortedSectionNames(s secrets.Store) []string {
	names := make([]string, 0, len(s))
	for n := range s {
		names = append(names, n)
	}
	// reuse secrets.ListKeysForSection's behavior: alphabetical
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	// avoid pulling sort directly here to keep deps minimal in this file
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func newSecretsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <section> <key> [<value>]",
		Short: "Set an env value in a section (for example channel.slack)",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			section := args[0]
			key := args[1]
			if section == "" || key == "" {
				return WithExit(2, errors.New("secrets set: section and key required"))
			}
			if !secrets.IsValidEnvKey(key) {
				return fmt.Errorf(`secrets set: key %q must be UPPER_SNAKE_CASE`, key)
			}
			var value string
			if len(args) == 3 {
				value = args[2]
			} else {
				if !prompt.IsInteractive() {
					return WithExit(2, fmt.Errorf("secrets set: value required when stdin is not a TTY (pass it as the third arg)"))
				}
				v, err := prompt.Hidden(key + " (hidden): ")
				if err != nil {
					return err
				}
				value = v
			}
			s, err := secrets.Read("")
			if err != nil {
				return err
			}
			existing := map[string]bool{}
			for _, k := range secrets.ListKeysForSection(s, section) {
				existing[k] = true
			}
			next, err := secrets.Set(s, section, key, value)
			if err != nil {
				return err
			}
			if err := secrets.Write(next, ""); err != nil {
				return err
			}
			verb := "updated"
			if !existing[key] {
				verb = "set"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s/%s\n", verb, section, key)
			return nil
		},
	}
}

func newSecretsUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <section> <key>",
		Short: "Remove an env value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			section := args[0]
			key := args[1]
			if section == "" || key == "" {
				return WithExit(2, errors.New("secrets unset: section and key required"))
			}
			s, err := secrets.Read("")
			if err != nil {
				return err
			}
			next, removed := secrets.Unset(s, section, key)
			out := cmd.OutOrStdout()
			if !removed {
				fmt.Fprintf(out, "%s/%s was not set\n", section, key)
				return nil
			}
			if err := secrets.Write(next, ""); err != nil {
				return err
			}
			fmt.Fprintf(out, "unset %s/%s\n", section, key)
			return nil
		},
	}
}
