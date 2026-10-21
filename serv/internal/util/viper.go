package util

import (
	"strings"

	"github.com/spf13/viper"
)

// SetKeyValue sets the value of a key in the viper config
func SetKeyValue(vi *viper.Viper, key string, value interface{}) bool {
	if strings.HasPrefix(key, "GJ_") || strings.HasPrefix(key, "SG_") || strings.HasPrefix(key, "SJ_") {
		key = key[3:]
	}
	uc := strings.Count(key, "_")
	k := strings.ToLower(key)

	if vi.Get(k) != nil {
		vi.Set(k, value)
		return true
	}

	for i := 0; i < uc; i++ {
		k = strings.Replace(k, "_", ".", 1)
		if vi.Get(k) != nil {
			vi.Set(k, value)
			return true
		}
	}

	// Config keys may themselves contain underscores (rate_limiter.rate,
	// databases.read_replica.host), so replacing only from the left cannot
	// find every configured path. Preserve the legacy matches above before
	// matching the environment spelling of existing canonical keys.
	envKey := strings.ToLower(key)
	var matched string
	for _, existing := range vi.AllKeys() {
		if strings.ReplaceAll(existing, ".", "_") != envKey {
			continue
		}
		if matched != "" {
			// Multiple paths have the same environment spelling. Do not
			// choose one based on map iteration order.
			return false
		}
		matched = existing
	}
	if matched != "" {
		vi.Set(matched, value)
		return true
	}
	return false
}
