package cmd

import (
	"path/filepath"

	"github.com/dosco/graphjin/internal/secrets"
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
		Run:     cmdServ(),
	}
	c.Flags().BoolVar(&deployActive, "deploy-active", false, "Deploy active config")
	return c
}

func cmdServ() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		setup(cpath)

		var opt []serv.Option
		if deployActive {
			opt = append(opt, serv.OptionDeployActive())
		}

		if conf.SecretsFile != "" {
			secFile, err := filepath.Abs(conf.RelPath(conf.SecretsFile))
			if err != nil {
				fatalInProd(err)
			}
			if err := secrets.Init(secFile); err != nil {
				fatalInProd(err)
			}
		}

		gj, err := serv.NewGraphJinService(conf, opt...)
		if err != nil {
			fatalInProd(err)
		}

		if err := gj.Start(); err != nil {
			fatalInProd(err)
		}
	}
}
