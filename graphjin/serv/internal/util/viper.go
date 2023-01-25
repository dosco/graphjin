package util

import (
	"strings"

	"github.com/spf13/viper"
)

func SetKeyValue(vi *viper.Viper, key string, value interface{}) bool {
	if strings.HasPrefix(key, "GJ_") || strings.HasPrefix(key, "SG_") {
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

	return false
}
