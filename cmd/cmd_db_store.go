package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

// dbStoreCmd manages the control-plane store (artifacts, watches, tasks).
func dbStoreCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "store",
		Short: "Manage the GraphJin control-plane store",
		Long: `The control-plane store holds GraphJin's own multi-user state: saved
artifacts, watches, tasks and their histories. Every GraphJin replica reads
and writes it, so replicas stay stateless.

Use 'db store init' to create or migrate its tables ahead of time, which lets
production run with artifacts.auto_init turned off.`,
	}

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create or migrate the control-plane store tables",
		Run:   cmdDBStoreInit,
	}
	initCmd.Flags().Bool("json", false, "Print the store status as JSON")
	c.AddCommand(initCmd)

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show where the control-plane store lives and which tables exist",
		Run:   cmdDBStoreStatus,
	}
	statusCmd.Flags().Bool("json", false, "Print the store status as JSON")
	c.AddCommand(statusCmd)

	return c
}

func cmdDBStoreInit(cmd *cobra.Command, args []string) {
	setup(cpath)
	st, err := serv.InitStore(conf)
	if err != nil {
		log.Fatal(err)
	}
	printStoreStatus(cmd, st)
}

func cmdDBStoreStatus(cmd *cobra.Command, args []string) {
	setup(cpath)
	st, err := serv.InspectStore(conf)
	if err != nil {
		log.Fatal(err)
	}
	printStoreStatus(cmd, st)
	if !st.Ready() {
		os.Exit(1)
	}
}

func printStoreStatus(cmd *cobra.Command, st serv.StoreStatus) {
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(st) //nolint:errcheck
		return
	}
	where := st.Source
	if st.Managed {
		where = "managed sqlite " + st.Path
	}
	fmt.Printf("store:  %s (%s), schema %s\n", where, st.Type, st.Schema)
	for _, t := range st.Tables {
		mark := "missing"
		if t.Exists {
			mark = "ok"
		}
		fmt.Printf("  %-14s %s\n", t.Name, mark)
	}
}
