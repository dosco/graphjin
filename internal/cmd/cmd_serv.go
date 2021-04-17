package cmd

import (
	"github.com/dosco/graphjin/serv"
	"github.com/spf13/cobra"
)

func cmdServ() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initCmd(cpath)

		s, err := serv.NewService(conf)
		if err != nil {
			fatalInProd(err)
		}

		if err := s.Start(); err != nil {
			fatalInProd(err)

		}
	}
}
