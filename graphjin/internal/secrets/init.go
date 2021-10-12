package secrets

import (
	"bytes"
	"os"
	"strings"

	"go.mozilla.org/sops/v3/aes"
	"go.mozilla.org/sops/v3/cmd/sops/common"
	"go.mozilla.org/sops/v3/keyservice"
	"go.mozilla.org/sops/v3/stores/dotenv"
)

func Init(fileName string) error {
	var err error

	inputStore := common.DefaultStoreForPath(fileName)
	ks := []keyservice.KeyServiceClient{keyservice.NewLocalClient()}

	opts := decryptOpts{
		OutputStore: &dotenv.Store{},
		InputStore:  inputStore,
		InputPath:   fileName,
		Cipher:      aes.NewCipher(),
		KeyServices: ks,
		IgnoreMAC:   false,
	}

	output, err := decrypt(opts)
	if err != nil {
		return err
	}

	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if line[0] == '#' {
			continue
		}
		v := strings.SplitN(string(line), "=", 2)
		os.Setenv(v[0], v[1])
	}

	return nil
}
