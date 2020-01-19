package serv

import (
	"github.com/spf13/cobra"
)

func cmdServ(cmd *cobra.Command, args []string) {
	var err error

	if conf, err = initConf(); err != nil {
		fatalInProd(err, "failed to read config")
	}

	if conf != nil {
		if db, err = initDBPool(conf); err != nil {
			fatalInProd(err, "failed to connect to database")
		}

		initCompiler()
		initAllowList(confPath)
		initPreparedList()
	}

	initWatcher(confPath)

	startHTTP()
}
