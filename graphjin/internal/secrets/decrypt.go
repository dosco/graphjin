package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"go.mozilla.org/sops/v3"
	"go.mozilla.org/sops/v3/cmd/sops/common"
	"go.mozilla.org/sops/v3/keyservice"
)

type decryptOpts struct {
	Cipher      sops.Cipher
	InputStore  sops.Store
	OutputStore sops.Store
	InputPath   string
	IgnoreMAC   bool
	Extract     []interface{}
	KeyServices []keyservice.KeyServiceClient
}

func decrypt(opts decryptOpts, fs afero.Fs) (decryptedFile []byte, err error) {
	tree, err := LoadEncryptedFileWithBugFixes(common.GenericDecryptOpts{
		Cipher:      opts.Cipher,
		InputStore:  opts.InputStore,
		InputPath:   opts.InputPath,
		IgnoreMAC:   opts.IgnoreMAC,
		KeyServices: opts.KeyServices,
	}, fs)

	if err != nil {
		return nil, err
	}

	_, err = common.DecryptTree(common.DecryptTreeOpts{
		Cipher:      opts.Cipher,
		IgnoreMac:   opts.IgnoreMAC,
		Tree:        tree,
		KeyServices: opts.KeyServices,
	})
	if err != nil {
		return nil, err
	}

	if len(opts.Extract) > 0 {
		return extract(tree, opts.Extract, opts.OutputStore)
	}

	decryptedFile, err = opts.OutputStore.EmitPlainFile(tree.Branches)
	if err != nil {
		return nil, err
	}
	return decryptedFile, err
}

func extract(tree *sops.Tree, path []interface{}, outputStore sops.Store) ([]byte, error) {
	var err error

	v, err := tree.Branches[0].Truncate(path)
	if err != nil {
		return nil, fmt.Errorf("error truncating tree: %s", err)
	}

	if newBranch, ok := v.(sops.TreeBranch); ok {
		tree.Branches[0] = newBranch
		decrypted, err := outputStore.EmitPlainFile(tree.Branches)
		if err != nil {
			return nil, err
		}
		return decrypted, err

	} else if str, ok := v.(string); ok {
		return []byte(str), nil
	}

	bytes, err := outputStore.EmitValue(v)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func LoadEncryptedFileWithBugFixes(
	opts common.GenericDecryptOpts,
	fs afero.Fs) (*sops.Tree, error) {

	tree, err := LoadEncryptedFile(opts.InputStore, opts.InputPath, fs)
	if err != nil {
		return nil, err
	}

	if tree == nil {
		return nil, errors.New("unable to decrypt file")
	}

	encCtxBug, err := common.DetectKMSEncryptionContextBug(tree)
	if err != nil {
		return nil, err
	}

	if encCtxBug {
		tree, err = common.FixAWSKMSEncryptionContextBug(opts, tree)
		if err != nil {
			return nil, err
		}
	}

	return tree, nil
}

func LoadEncryptedFile(
	loader sops.EncryptedFileLoader,
	inputPath string,
	fs afero.Fs) (*sops.Tree, error) {

	var fileBytes []byte
	var err error

	if fs == nil {
		fileBytes, err = os.ReadFile(inputPath)
	} else {
		fileBytes, err = afero.ReadFile(fs, inputPath)
	}
	if err != nil {
		return nil, err
	}

	path, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, err
	}

	tree, err := loader.LoadEncryptedFile(fileBytes)
	if err != nil {
		return nil, err
	}

	tree.FilePath = path
	return &tree, nil
}
