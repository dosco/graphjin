package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/open-neko/neko/apps/openneko/internal/compose"
)

// The worker already owns GraphJin preview/apply, credential sealing, the
// shared config lock, persistence, and restart. Use that path from the host.
const graphjinImportScript = `
import { getOrgId } from "@neko/db";
import { applyPackGraphjinConfig } from "./src/packs/graphjin-config.ts";
let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const update = JSON.parse(raw);
await applyPackGraphjinConfig({
  endpoint: "http://openneko-serve:8080/api/v1/graphql",
  orgId: await getOrgId(),
  configFile: process.env.OPENNEKO_GRAPHJIN_CONFIG,
  update,
  restartAfterPersist: true,
});
console.log("GraphJin configuration imported");
`

func newGraphjinCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "graphjin", Short: "Manage the customer GraphJin configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "import <customer-config.yml|->",
		Short: "Import customer sources, tables, and relationships into the running instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var input []byte
			var err error
			if args[0] == "-" {
				input, err = io.ReadAll(cmd.InOrStdin())
			} else {
				input, err = os.ReadFile(args[0])
			}
			if err != nil {
				return err
			}
			update, err := graphjinImportUpdate(input)
			if err != nil {
				return err
			}
			settings, installed, err := loadCurrentInstallation()
			if err != nil {
				return err
			}
			if !installed || compose.Mode(settings.Mode) != compose.ModeProd {
				return fmt.Errorf("graphjin import requires a selected production installation")
			}
			var worker string
			if code, proxied := MaybeProxyToWorker(cmd, func(container string) int {
				worker = container
				return 0
			}); proxied && code != 0 {
				return WithExit(code, nil)
			}
			if worker == "" {
				return fmt.Errorf("graphjin import requires a running instance worker")
			}
			process := exec.CommandContext(cmd.Context(), "docker", "exec", "-i", worker,
				"node", "--import", "tsx/esm", "--input-type=module", "--eval", graphjinImportScript)
			process.Stdin = bytes.NewReader(update)
			process.Stdout = cmd.OutOrStdout()
			process.Stderr = cmd.ErrOrStderr()
			if err := process.Run(); err != nil {
				return fmt.Errorf("graphjin import: %w", err)
			}
			return nil
		},
	})
	return cmd
}

func graphjinImportUpdate(input []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(input, &document); err != nil {
		return nil, fmt.Errorf("invalid GraphJin YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("GraphJin import must be a YAML object")
	}
	var sections map[string]any
	if err := document.Decode(&sections); err != nil {
		return nil, err
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("GraphJin import is empty")
	}
	update := make(map[string]any, len(sections))
	for key, value := range sections {
		switch key {
		case "sources", "tables", "relationships":
			items, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("GraphJin %s must be a list", key)
			}
			if key == "sources" {
				key = "update_sources"
			}
			update[key] = items
		default:
			return nil, fmt.Errorf("GraphJin import cannot change managed setting %q; use a file containing only sources, tables, and relationships", strings.TrimSpace(key))
		}
	}
	return json.Marshal(update)
}
