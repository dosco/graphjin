package cmd

import (
	"github.com/spf13/cobra"
)

func cmdServ() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)

		if err := s.Start(); err != nil {
			fatalInProd(err)

		}
	}
}
