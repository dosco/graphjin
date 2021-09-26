package cmd

import (
	"github.com/dosco/graphjin/serv"
	"github.com/spf13/cobra"
)

func servCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "serv",
		Short: "Run the GraphJin service",
		Run:   cmdServ(),
	}
	return c
}

func cmdServ() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		setup(cpath)

		gj, err := serv.NewGraphJinService(conf)
		if err != nil {
			fatalInProd(err)
		}

		if err := gj.Start(); err != nil {
			fatalInProd(err)
		}
	}
}
