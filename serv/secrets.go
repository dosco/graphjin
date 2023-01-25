package serv

import (
	"github.com/dosco/graphjin/serv/v3/internal/secrets"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type SecretArgs struct {
	KMS, KMSC, AWS, GCP, Azure, PGP string
}

func SecretsCmd(cmdName, fileName string, sa SecretArgs, args []string, log *zap.SugaredLogger) error {
	return secrets.SecretsCmd(
		cmdName, fileName, secrets.SecretArgs(sa), args, log)
}

func initSecrets(secFile string, fs afero.Fs) (map[string]string, error) {
	return secrets.Init(secFile, fs)
}
