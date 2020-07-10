package serv

import (
	"github.com/dosco/super-graph/core"
	"github.com/spf13/cobra"
)

var (
	sg *core.SuperGraph
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

		sg, err = core.NewSuperGraph(&servConf.conf.Core, servConf.db)
		if err != nil {
			fatalInProd(servConf, err, "failed to initialize Super Graph")
		}

		startHTTP(servConf)
	}
}
