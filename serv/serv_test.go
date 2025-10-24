package serv_test

import (
	"testing"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestServe(t *testing.T) {
	t.Run("readInConfigWithEnvVars", readInConfigWithEnvVars)
}

// nolint:errcheck
func readInConfigWithEnvVars(t *testing.T) {
	devConfig := "app_name: \"App Name\"\n"
	prodConfig := "inherits: dev\n"
	stageConfig := "inherits: dev\n"

	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/dev.yml", []byte(devConfig), 0o666)
	afero.WriteFile(fs, "/prod.yml", []byte(prodConfig), 0o666)
	afero.WriteFile(fs, "/stage.yml", []byte(stageConfig), 0o666)

	c, err := serv.ReadInConfigFS("/dev.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "App Name", c.AppName)

	c, err = serv.ReadInConfigFS("/prod.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "App Name", c.AppName)

	c, err = serv.ReadInConfigFS("/stage.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "App Name", c.AppName)
}
