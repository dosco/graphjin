package cmd

import (
	"fmt"
	"math/rand"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/dosco/graphjin/internal/client"
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
	c.PersistentFlags().StringVar(&host, "host", "", "URL of the GraphJin service")
	c.PersistentFlags().StringVar(&name, "name", "", "Set a custom name for the deployment")
	c.PersistentFlags().StringVar(&secret, "secret", "", "Set the admin auth secret key")

	c1 := &cobra.Command{
		Use:   "rollback",
		Short: "Rollback to the previous active config",
		Run:   cmdRollback(),
	}
	c.AddCommand(c1)

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

func cmdDeploy() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if host == "" {
			log.Fatalf("--host is a required argument")
		}

		if secret == "" {
			log.Fatalf("--secret is a required argument")
		}

		if name == "" {
			// #nosec G404
			name = slug.Make(fmt.Sprintf("%s-%d", gofakeit.Name(), rand.Intn(9)))
		}

		c := client.NewClient(host, secret)

		if res, err := c.Deploy(name, "./config"); err != nil {
			log.Fatal(err)
		} else {
			log.Infof(res.Msg)
		}
	}
}

func cmdRollback() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if host == "" {
			log.Fatalf("--host is a required argument")
		}

		if name != "" {
			log.Fatalf("--name not supported with rollback")
		}

		c := client.NewClient(host, secret)

		if res, err := c.Rollback(); err != nil {
			log.Fatal(err)
		} else {
			log.Infof(res.Msg)
		}
	}
}
