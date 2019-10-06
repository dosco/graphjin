package serv

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func cmdConfDump(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help()
		os.Exit(1)
	}

	fname := fmt.Sprintf("%s.%s", getConfigName(), args[0])

	vi := newConfig()

	if err := vi.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Send()
	}

	if err := vi.WriteConfigAs(fname); err != nil {
		logger.Fatal().Err(err).Send()
	}

	logger.Info().Msgf("config dumped to ./%s", fname)
}
