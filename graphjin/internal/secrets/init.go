package secrets

import (
	"bytes"
	"strings"

	"github.com/spf13/afero"
	"go.mozilla.org/sops/v3/aes"
	"go.mozilla.org/sops/v3/cmd/sops/common"
	"go.mozilla.org/sops/v3/keyservice"
	"go.mozilla.org/sops/v3/stores/dotenv"
)

func Init(filename string, fs afero.Fs) (map[string]string, error) {
	return initSecrets(filename, fs)
}

func initSecrets(filename string, fs afero.Fs) (map[string]string, error) {
	var err error

	inputStore := common.DefaultStoreForPath(filename)
	ks := []keyservice.KeyServiceClient{keyservice.NewLocalClient()}

	opts := decryptOpts{
		OutputStore: &dotenv.Store{},
		InputStore:  inputStore,
		InputPath:   filename,
		Cipher:      aes.NewCipher(),
		KeyServices: ks,
		IgnoreMAC:   false,
	}

	output, err := decrypt(opts, fs)
	if err != nil {
		return nil, err
	}

	res := make(map[string]string)

	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if line[0] == '#' {
			continue
		}
		v := strings.SplitN(string(line), "=", 2)
		res[v[0]] = v[1]
	}

	return res, nil
}
