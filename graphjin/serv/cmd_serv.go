package serv

import (
	"github.com/spf13/cobra"
)

func cmdServ(cmd *cobra.Command, args []string) {
	var err error

	initWatcher(confPath)

	if conf, err = initConf(); err != nil {
		fatalInProd(err, "failed to read config")
	}

	db, err = initDBPool(conf)

	if err != nil {
		fatalInProd(err, "failed to connect to database")
	}

	initCompiler()
	initResolvers()
	initAllowList(confPath)
	initPreparedList(confPath)

	startHTTP()
}
