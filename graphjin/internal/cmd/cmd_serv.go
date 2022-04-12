package cmd

import (
	"github.com/dosco/graphjin/serv"
	"github.com/spf13/cobra"
)

var (
	deployActive bool
)

func servCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"serv"},
		Short:   "Run the GraphJin service",
		RunE:    cmdServ,
	}
	c.Flags().BoolVar(&deployActive, "deploy-active", false, "Deploy active config")
	return c
}

func cmdServ(*cobra.Command, []string) error {
	setup(cpath)

	var opt []serv.Option
	if deployActive {
		opt = append(opt, serv.OptionDeployActive())
	}

	gj, err := serv.NewGraphJinService(conf, opt...)
	if err != nil {
		return err
	}

	if err := gj.Start(); err != nil {
		return err
	}
	return nil
}
