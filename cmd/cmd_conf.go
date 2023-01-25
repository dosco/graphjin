package main

// func cmdConfDump(cmd *cobra.Command, args []string) {
// 	if len(args) != 1 {
// 		cmd.Help() //nolint:errcheck
// 		os.Exit(1)
// 	}

// 	fname := fmt.Sprintf("%s.%s", config.GetConfigName(), args[0])

// 	conf, err := initConf()
// 	if err != nil {
// 		log.Fatalf("ERR failed to read config: %s", err)
// 	}

// 	if err := conf.WriteConfigAs(fname); err != nil {
// 		log.Fatalf("ERR failed to write config: %s", err)
// 	}

// 	log.Printf("INF config dumped to ./%s", fname)
// }
