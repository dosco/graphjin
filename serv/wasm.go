//go:build wasm

package serv

import (
	"github.com/spf13/afero"
)

func startConfigWatcher(s1 *Service) error {
	return nil
}

func initSecrets(secFile string, fs afero.Fs) (map[string]string, error) {
	return nil, nil
}
