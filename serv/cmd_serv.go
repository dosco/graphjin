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
		db, err = initDBPool(conf)

		if err == nil {
			initCompiler()
			initAllowList(confPath)
			initPreparedList()
		} else {
			fatalInProd(err, "failed to connect to database")
		}
	}

	initWatcher(confPath)

	startHTTP()
}
