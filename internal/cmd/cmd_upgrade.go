package cmd

import (
	core "github.com/dosco/graphjin/v2/core"
	"github.com/spf13/cobra"
)

func upgradeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a GraphJin app",
		Run:   cmdUpgrade,
	}
	return c
}

func cmdUpgrade(cmd *cobra.Command, args []string) {
	if err := core.Upgrade(cpath); err != nil {
		log.Fatalf("%s", err)
	}
	log.Infof("please delete the .yml/.yaml query and old fragment files under %s/queries", cpath)
	log.Infoln("upgrade completed!")
}
