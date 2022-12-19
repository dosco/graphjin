//go:build !wasm

package serv

import (
	"github.com/dosco/graphjin/v2/internal/secrets"
	"github.com/spf13/afero"
)

func initSecrets(secFile string, fs afero.Fs) (map[string]string, error) {
	return secrets.Init(secFile, fs)
}
