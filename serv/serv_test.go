package serv_test

import (
	"os"
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
	devConfig := "app_name: \"App Name\"\nsecrets_file: dev.secrets.json\n"
	prodConfig := "inherits: dev\nsecrets_file: \"prod.secrets.json\"\n"
	stageConfig := "inherits: dev\nsecrets_file: \"\"\n"
	secrets := ``

	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/dev.yml", []byte(devConfig), 0o666)
	afero.WriteFile(fs, "/prod.yml", []byte(prodConfig), 0o666)
	afero.WriteFile(fs, "/stage.yml", []byte(stageConfig), 0o666)

	afero.WriteFile(fs, "/dev.secrets.json", []byte(secrets), 0o666)
	afero.WriteFile(fs, "/prod.secrets.json", []byte(secrets), 0o666)

	_, err := serv.ReadInConfigFS("/dev.yml", fs)
	assert.ErrorContains(t, err, "dev.secrets.json")

	_, err = serv.ReadInConfigFS("/prod.yml", fs)
	assert.ErrorContains(t, err, "prod.secrets.json")

	os.Setenv("GJ_SECRETS_FILE", "new.dev.secrets.json")
	_, err = serv.ReadInConfigFS("/dev.yml", fs)
	assert.ErrorContains(t, err, "new.dev.secrets.json")

	os.Setenv("GJ_SECRETS_FILE", "new.prod.secrets.json")
	_, err = serv.ReadInConfigFS("/prod.yml", fs)
	assert.ErrorContains(t, err, "new.prod.secrets.json")

	os.Unsetenv("GJ_SECRETS_FILE")
	c, err := serv.ReadInConfigFS("/stage.yml", fs)
	assert.NoError(t, err)
	assert.Equal(t, "App Name", c.AppName)
}
