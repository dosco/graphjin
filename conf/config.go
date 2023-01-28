package conf

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/dosco/graphjin/core/v3"
	"gopkg.in/yaml.v3"
)

type configInfo struct {
	Inherits string
}

func NewConfig(configPath, configFile string) (c *core.Config, err error) {
	fs := core.NewOsFS(configPath)
	if c, err = NewConfigWithFS(fs, configFile); err != nil {
		return
	}
	return
}

func NewConfigWithFS(fs core.FS, configFile string) (c *core.Config, err error) {
	c = &core.Config{FS: fs}
	var ci configInfo

	if err = readConfig(fs, configFile, &ci); err != nil {
		return
	}

	if ci.Inherits != "" {
		pc := ci.Inherits

		if filepath.Ext(pc) == "" {
			pc += filepath.Ext(configFile)
		}

		if err = readConfig(fs, pc, &c); err != nil {
			return
		}
	}

	if err = readConfig(fs, configFile, &c); err != nil {
		return
	}
	return
}

func readConfig(fs core.FS, configFile string, v interface{}) (err error) {
	format := filepath.Ext(configFile)

	b, err := fs.Get(configFile)
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
