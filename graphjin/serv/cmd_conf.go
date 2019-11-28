package serv

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func cmdConfDump(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help() //nolint: errcheck
		os.Exit(1)
	}

	fname := fmt.Sprintf("%s.%s", getConfigName(), args[0])

	conf, err := initConf()
	if err != nil {
		errlog.Fatal().Err(err).Msg("failed to read config")
	}

	if err := conf.Viper.WriteConfigAs(fname); err != nil {
		errlog.Fatal().Err(err).Send()
	}

	logger.Info().Msgf("config dumped to ./%s", fname)
}
