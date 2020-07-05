package serv

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// ReadInConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new Super Graph config.
func ReadInConfig(configFile string) (*Config, error) {
	cpath := path.Dir(configFile)
	cfile := path.Base(configFile)
	vi := newViper(cpath, cfile)

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	inherits := vi.GetString("inherits")

	if inherits != "" {
		vi = newViper(cpath, inherits)

		if err := vi.ReadInConfig(); err != nil {
			return nil, err
		}

		if vi.IsSet("inherits") {
			return nil, fmt.Errorf("inherited config (%s) cannot itself inherit (%s)",
				inherits,
				vi.GetString("inherits"))
		}

		vi.SetConfigName(cfile)

		if err := vi.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	c := &Config{cpath: cpath, vi: vi}

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	return c, nil
}

func newViper(configPath, configFile string) *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.AddConfigPath(configPath)
	vi.SetConfigName(configFile)
	vi.AddConfigPath("./config")

	vi.SetDefault("host_port", "0.0.0.0:8080")
	vi.SetDefault("web_ui", false)
	vi.SetDefault("enable_tracing", false)
	vi.SetDefault("auth_fail_block", false)
	vi.SetDefault("seed_file", "seed.js")

	vi.SetDefault("default_block", true)

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")
	vi.SetDefault("database.schema", "public")

	vi.SetDefault("env", "development")

	vi.BindEnv("env", "GO_ENV") //nolint: errcheck
	vi.BindEnv("host", "HOST")  //nolint: errcheck
	vi.BindEnv("port", "PORT")  //nolint: errcheck

	vi.SetDefault("auth.rails.max_idle", 80)
	vi.SetDefault("auth.rails.max_active", 12000)

	return vi
}

func GetConfigName() string {
	if os.Getenv("GO_ENV") == "" {
		return "dev"
	}

	ge := strings.ToLower(os.Getenv("GO_ENV"))

	switch {
	case strings.HasPrefix(ge, "pro"):
		return "prod"

	case strings.HasPrefix(ge, "sta"):
		return "stage"

	case strings.HasPrefix(ge, "tes"):
		return "test"

	case strings.HasPrefix(ge, "dev"):
		return "dev"
	}

	return ge
}

func (c *Config) telemetryEnabled() bool {
	return c.Telemetry.Debug || c.Telemetry.Metrics.Exporter != "" || c.Telemetry.Tracing.Exporter != ""
}

func (c *Config) relPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}

	return path.Join(c.cpath, p)
}

func (c *Config) rateLimiterEnable() bool {
	log.Println("Rate", c.RateLimiter.Rate, " Bucket ", c.RateLimiter.Bucket)
	return c.RateLimiter.Rate > 0 && c.RateLimiter.Bucket > 0
}
