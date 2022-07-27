package core_test

import (
	"os"
	"testing"

	"github.com/dosco/graphjin/core"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestCore(t *testing.T) {
	t.Run("readInConfigWithEnvVars", readInConfigWithEnvVars)
}

// nolint: errcheck
func readInConfigWithEnvVars(t *testing.T) {
	devConfig := "secret_key: dev_secret_key\n"
	prodConfig := "inherits: dev\nsecret_key: \"prod_secret_key\"\n"

	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/dev.yml", []byte(devConfig), 0666)
	afero.WriteFile(fs, "/prod.yml", []byte(prodConfig), 0666)

	c, err := core.ReadInConfigFS("/dev.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "dev_secret_key", c.SecretKey)

	c, err = core.ReadInConfigFS("/prod.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "prod_secret_key", c.SecretKey)

	os.Setenv("GJ_SECRET_KEY", "new_dev_secret_key")
	c, err = core.ReadInConfigFS("/dev.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "new_dev_secret_key", c.SecretKey)

	os.Setenv("GJ_SECRET_KEY", "new_prod_secret_key")
	c, err = core.ReadInConfigFS("/prod.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "new_prod_secret_key", c.SecretKey)

	os.Unsetenv("GJ_SECRET_KEY")
}
