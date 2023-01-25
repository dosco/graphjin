package main

import (
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var deployActive bool

func servCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"serv"},
		Short:   "Run the GraphJin service",
		Run:     cmdServ,
	}
	c.Flags().BoolVar(&deployActive, "deploy-active", false, "Deploy active config")
	return c
}

func cmdServ(*cobra.Command, []string) {
	setup(cpath)

	var opt []serv.Option
	if deployActive {
		opt = append(opt, serv.OptionDeployActive())
	}

	gj, err := serv.NewGraphJinService(conf, opt...)
	if err != nil {
		log.Fatalf("%s", err)
	}

	if err := gj.Start(); err != nil {
		log.Fatalf("%s", err)
	}
}
