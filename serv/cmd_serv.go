package serv

import (
	"github.com/spf13/cobra"
)

func cmdServ(cmd *cobra.Command, args []string) {
	var err error

	if conf, err = initConf(); err != nil {
		errlog.Fatal().Err(err).Msg("failed to read config")
	}

	db, err = initDBPool(conf)
	if err != nil {
		errlog.Fatal().Err(err).Msg("failed to connect to database")
	}

	initCompiler()
	initAllowList(confPath)
	initPreparedList()
	initWatcher(confPath)

	startHTTP()
}
