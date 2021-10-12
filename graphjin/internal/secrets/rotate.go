package secrets

import (
	"fmt"

	"go.mozilla.org/sops/v3"
	"go.mozilla.org/sops/v3/audit"
	"go.mozilla.org/sops/v3/cmd/sops/codes"
	"go.mozilla.org/sops/v3/cmd/sops/common"
	"go.mozilla.org/sops/v3/keys"
	"go.mozilla.org/sops/v3/keyservice"
	"go.uber.org/zap"
)

type rotateOpts struct {
	log              *zap.SugaredLogger
	Cipher           sops.Cipher
	InputStore       sops.Store
	OutputStore      sops.Store
	InputPath        string
	IgnoreMAC        bool
	AddMasterKeys    []keys.MasterKey
	RemoveMasterKeys []keys.MasterKey
	KeyServices      []keyservice.KeyServiceClient
}

func rotate(opts rotateOpts) ([]byte, error) {
	tree, err := common.LoadEncryptedFileWithBugFixes(common.GenericDecryptOpts{
		Cipher:      opts.Cipher,
		InputStore:  opts.InputStore,
		InputPath:   opts.InputPath,
		IgnoreMAC:   opts.IgnoreMAC,
		KeyServices: opts.KeyServices,
	})
	if err != nil {
		return nil, err
	}

	audit.SubmitEvent(audit.RotateEvent{
		File: tree.FilePath,
	})

	_, err = common.DecryptTree(common.DecryptTreeOpts{
		Cipher: opts.Cipher, IgnoreMac: opts.IgnoreMAC, Tree: tree,
		KeyServices: opts.KeyServices,
	})
	if err != nil {
		return nil, err
	}

	// Add new master keys
	for _, key := range opts.AddMasterKeys {
		tree.Metadata.KeyGroups[0] = append(tree.Metadata.KeyGroups[0], key)
	}
	// Remove master keys
	for _, rmKey := range opts.RemoveMasterKeys {
		for i := range tree.Metadata.KeyGroups {
			for j, groupKey := range tree.Metadata.KeyGroups[i] {
				if rmKey.ToString() == groupKey.ToString() {
					tree.Metadata.KeyGroups[i] = append(tree.Metadata.KeyGroups[i][:j], tree.Metadata.KeyGroups[i][j+1:]...)
				}
			}
		}
	}

	// Create a new data key
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		err = fmt.Errorf("Could not generate data key: %s", errs)
		return nil, err
	}

	// Reencrypt the file with the new key
	err = common.EncryptTree(common.EncryptTreeOpts{
		DataKey: dataKey, Tree: tree, Cipher: opts.Cipher,
	})
	if err != nil {
		return nil, err
	}

	encryptedFile, err := opts.OutputStore.EmitEncryptedFile(*tree)
	if err != nil {
		return nil, common.NewExitError(fmt.Sprintf("Could not marshal tree: %s", err), codes.ErrorDumpingTree)
	}
	return encryptedFile, nil
}

/*
func keyservices(sa sopArgs) (svcs []keyservice.KeyServiceClient) {
	uris := c.StringSlice("keyservice")
	for _, uri := range uris {
		url, err := url.Parse(uri)
		if err != nil {
			log.Warnf("Error parsing URI for keyservice, skipping: %s", uri)
			continue
		}

		addr := url.Host
		if url.Scheme == "unix" {
			addr = url.Path
		}
		opts := []grpc.DialOption{
			grpc.WithInsecure(),
			grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
				return net.DialTimeout(url.Scheme, addr, timeout)
			}),
		}
		log.Infof("Connecting to key service: %s", fmt.Sprintf("%s://%s", url.Scheme, addr))
		conn, err := grpc.Dial(addr, opts...)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		svcs = append(svcs, keyservice.NewKeyServiceClient(conn))
	}
	return
}
*/
