package serv

import (
	"github.com/dosco/super-graph/core"
	"github.com/spf13/cobra"
)

var (
	sg *core.SuperGraph
)

func cmdServ(cmd *cobra.Command, args []string) {
	var err error

	conf, err = initConf()
	if err != nil {
		fatalInProd(err, "failed to read config")
	}

	initWatcher()

	db, err = initDB(conf)
	if err != nil {
		fatalInProd(err, "failed to connect to database")
	}

	// if conf != nil && db != nil {
	// 	initResolvers()
	// }

	sg, err = core.NewSuperGraph(&conf.Core, db)
	if err != nil {
		fatalInProd(err, "failed to initialize Super Graph")
	}

	startHTTP()
}
