//go:build wasm

package cmd

import "github.com/spf13/cobra"

func cmdSecrets() *cobra.Command {
	return nil
}

func initSecrets(secFile string) (bool, error) {
	return false, nil
}
