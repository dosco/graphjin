package cmd

import (
	"fmt"
	"math/rand"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/dosco/graphjin/serv"
	"github.com/gosimple/slug"
	"github.com/spf13/cobra"
)

var (
	host   string
	name   string
	secret string
)

func deployCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a new config",
		Run:   cmdDeploy(),
	}
	c.Flags().StringVar(&host, "host", "", "URL of the GraphJin service")
	c.Flags().StringVar(&name, "name", "", "Set a custom name for the deployment")
	c.Flags().StringVar(&secret, "secret", "", "Set the admin auth secret key")

	return c
}

func initCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "init",
		Short: "Setup admin database",
		Run:   cmdInit(),
	}
	return c
}

func cmdDeploy() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if host == "" {
			log.Fatalf("--host is a required argument")
		}

		if secret == "" {
			log.Fatalf("--secret is a required argument")
		}

		if name == "" {
			name = slug.Make(fmt.Sprintf("%s-%d", gofakeit.Name(), rand.Intn(9)))
		}

		c := serv.NewClient(host, secret)

		if err := c.Deploy(name, "./config"); err != nil {
			log.Fatal(err)
		}

		log.Infof("deploy successful: %s", name)
	}
}

func cmdInit() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		setup(cpath)
		initDB(true)

		if err := serv.InitAdmin(db, conf.DBType); err != nil {
			log.Fatal(err)
		}

		log.Infof("init successful: %s", name)
	}

}
