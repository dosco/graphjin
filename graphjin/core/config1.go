package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/dosco/graphjin/v2/plugin"
	"gopkg.in/yaml.v3"
)

type configInfo struct {
	Inherits string
}

func NewConfig(fs plugin.FS, configFile string) (*Config, error) {
	var c Config
	var ci configInfo

	if err := readConfig(fs, configFile, &ci); err != nil {
		return nil, err
	}

	if ci.Inherits != "" {
		pc := ci.Inherits

		if filepath.Ext(pc) == "" {
			pc += filepath.Ext(configFile)
		}

		if err := readConfig(fs, pc, &c); err != nil {
			return nil, err
		}
	}

	if err := readConfig(fs, configFile, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

func readConfig(fs plugin.FS, configFile string, v interface{}) (err error) {
	format := filepath.Ext(configFile)

	b, err := fs.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("error reading config: %w", err)
	}

	switch format {
	case ".json":
		err = json.Unmarshal(b, v)
	case ".yml", ".yaml":
		err = yaml.Unmarshal(b, v)
	default:
		err = fmt.Errorf("invalid format %s", format)
	}

	if err != nil {
		err = fmt.Errorf("error reading config: %w", err)
	}
	return
}
