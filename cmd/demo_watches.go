package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/serv/v3"
	"gopkg.in/yaml.v3"
)

// demoWatchSeedFile is the project's declared standing questions, relative to
// the config directory. A project without one simply seeds no watches.
const demoWatchSeedFile = "seed/watches.yml"

type demoWatchSeedDoc struct {
	Operator demoWatchSeedOperator `yaml:"operator"`
	Watches  []demoWatchSeedEntry  `yaml:"watches"`
}

type demoWatchSeedOperator struct {
	UserID      string `yaml:"user_id"`
	Role        string `yaml:"role"`
	ConsoleRole string `yaml:"console_role"`
	AccountID   string `yaml:"account_id"`
}

type demoWatchSeedEntry struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Query       string         `yaml:"query"`
	SavedQuery  string         `yaml:"saved_query_name"`
	Variables   map[string]any `yaml:"variables"`
	Delivery    map[string]any `yaml:"delivery"`
	Absence     map[string]any `yaml:"absence"`
	Enrich      map[string]any `yaml:"enrich"`
}

// loadDemoWatchSeeds reads the project's declared standing watches. A missing
// file is not an error: most projects have none.
func loadDemoWatchSeeds(configPath string) (serv.OperatorSeed, bool, error) {
	path := filepath.Join(configPath, filepath.FromSlash(demoWatchSeedFile))
	// Assert the extension rather than relying on the data seeder ignoring it:
	// seed/<name>.<ext> otherwise reads as "seed the <name> source".
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".yml" && ext != ".yaml" {
		return serv.OperatorSeed{}, false, fmt.Errorf("watch seed file must be YAML: %s", path)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return serv.OperatorSeed{}, false, nil
		}
		return serv.OperatorSeed{}, false, err
	}

	var doc demoWatchSeedDoc
	if err := yaml.Unmarshal(buf, &doc); err != nil {
		return serv.OperatorSeed{}, false, fmt.Errorf("%s: %w", demoWatchSeedFile, err)
	}
	if strings.TrimSpace(doc.Operator.UserID) == "" {
		return serv.OperatorSeed{}, false, fmt.Errorf("%s: operator.user_id is required", demoWatchSeedFile)
	}

	seed := serv.OperatorSeed{
		UserID:      doc.Operator.UserID,
		Role:        doc.Operator.Role,
		ConsoleRole: doc.Operator.ConsoleRole,
		AccountID:   doc.Operator.AccountID,
	}
	for _, entry := range doc.Watches {
		watch := serv.WatchSeed{
			Name:           entry.Name,
			Description:    entry.Description,
			Query:          entry.Query,
			SavedQueryName: entry.SavedQuery,
		}
		for _, field := range []struct {
			value map[string]any
			out   *string
			name  string
		}{
			{entry.Variables, &watch.VariablesJSON, "variables"},
			{entry.Delivery, &watch.DeliveryJSON, "delivery"},
			{entry.Absence, &watch.AbsenceJSON, "absence"},
			{entry.Enrich, &watch.EnrichJSON, "enrich"},
		} {
			encoded, err := demoWatchSeedJSON(field.value)
			if err != nil {
				return serv.OperatorSeed{}, false, fmt.Errorf("%s: watch %q %s: %w",
					demoWatchSeedFile, entry.Name, field.name, err)
			}
			*field.out = encoded
		}
		seed.Watches = append(seed.Watches, watch)
	}
	return seed, true, nil
}

// demoWatchSeedJSON renders a declarative block into the JSON string column the
// gj_watch mutation expects. An empty block stays empty so the watch keeps the
// server-side default.
func demoWatchSeedJSON(value map[string]any) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	out, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// demoOperatorSeed returns what a demo boot should apply for this project.
//
// The two halves have different lifetimes. The operator identity is a stable
// property of the project, so it is offered on every boot — that is what lets
// the console keep starting inside an owner scope that has content. The watches
// are created only on a fresh provision, following the same rule as seeding
// data, so a reused demo keeps whatever the operator changed. That includes
// watches they deleted: the seeder cannot tell a deleted watch from an unseeded
// one, so it must never be asked on a reused demo.
func demoOperatorSeed(demo *DemoRuntime, configPath string) (serv.OperatorSeed, bool, error) {
	if demo == nil {
		return serv.OperatorSeed{}, false, nil
	}
	seed, ok, err := loadDemoWatchSeeds(configPath)
	if err != nil || !ok {
		return serv.OperatorSeed{}, false, err
	}
	if !demo.FirstRun {
		seed.Watches = nil
	}
	return seed, true, nil
}

// demoWatchSeedOption wires the project's operator identity and declared
// standing watches into service startup, routing per-watch progress into the
// demo status stream.
func demoWatchSeedOption(demo *DemoRuntime, configPath string) (serv.Option, bool) {
	seed, ok, err := demoOperatorSeed(demo, configPath)
	if err != nil {
		demo.Status.Emit("watch-seed", "failed", err.Error())
		return nil, false
	}
	if !ok {
		return nil, false
	}
	status := demo.Status
	seed.Report = func(name, state, msg string) {
		if strings.TrimSpace(name) != "" {
			msg = strings.TrimSpace(name + " " + msg)
		}
		status.Emit("watch-seed", state, msg)
	}
	return serv.OptionSetOperatorSeed(seed), true
}
