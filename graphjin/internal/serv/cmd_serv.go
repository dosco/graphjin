package serv

import (
	"github.com/dosco/graphjin/core"
	"github.com/spf13/cobra"
)

var (
	gj *core.GraphJin
)

func cmdServ(servConf *ServConfig) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		var err error

		servConf.conf, err = initConf(servConf)
		if err != nil {
			fatalInProd(servConf, err, "failed to read config")
		}

		initWatcher(servConf)

		servConf.db, err = initDB(servConf, true, true)
		if err != nil {
			fatalInProd(servConf, err, "failed to connect to database")
		}

		servConf.zlog = newLogger(servConf)

		gj, err = core.NewGraphJin(&servConf.conf.Core, servConf.db)
		if err != nil {
			fatalInProd(servConf, err, "failed to initialize")
		}

		startHTTP(servConf)
	}
}
