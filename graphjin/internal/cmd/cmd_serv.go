package cmd

import (
	"path/filepath"

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

	var fsec bool
	if conf.SecretsFile != "" {
		secFile, err := filepath.Abs(conf.RelPath(conf.SecretsFile))
		if err != nil {
			return err
		}
		if fsec, err = initSecrets(secFile); err != nil {
			return err
		}
	}

	if fsec {
		setupAgain(cpath)
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
