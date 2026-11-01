package config

import (
	"os"
	"path/filepath"

	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

const AppDirName = "openneko"

func Dir(override string) string {
	if override != "" {
		return override
	}
	return instance.ScopeConfigDir(RootDir())
}

// RootDir returns the unscoped OpenNeko config directory. Most callers should
// use Dir; inventory commands use RootDir to discover named installations.
func RootDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, AppDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".config", AppDirName)
}

func File(override, name string) string {
	return filepath.Join(Dir(override), name)
}
