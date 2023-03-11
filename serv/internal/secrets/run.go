package secrets

import (
	"fmt"
	"os"

	"go.mozilla.org/sops/v3"
	"go.mozilla.org/sops/v3/aes"
	_ "go.mozilla.org/sops/v3/audit"
	"go.mozilla.org/sops/v3/azkv"
	"go.mozilla.org/sops/v3/cmd/sops/common"
	"go.mozilla.org/sops/v3/gcpkms"
	"go.mozilla.org/sops/v3/keys"
	"go.mozilla.org/sops/v3/keyservice"
	"go.mozilla.org/sops/v3/kms"
	"go.mozilla.org/sops/v3/pgp"
	"go.uber.org/zap"
)

type SecretArgs struct {
	KMS, KMSC, AWS, GCP, Azure, PGP string //nolint:golint,unused
}

func SecretsCmd(cmdName, fileName string, sa SecretArgs, args []string, log *zap.SugaredLogger) error {
	var err error

	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		if (cmdName == "add-master-key" || cmdName == "remove-master-key") &&
			(sa.PGP != "" || sa.GCP != "" || sa.Azure != "" || sa.AWS != "") {
			return fmt.Errorf("cannot add or remove keys on non-existent files")
		}
		if cmdName == "encrypt" || cmdName == "decrypt" || cmdName == "rotate" {
			return fmt.Errorf("cannot operate on non-existent file")
		}
	}

	ks := []keyservice.KeyServiceClient{keyservice.NewLocalClient()}
	inputStore := common.DefaultStoreForPath(fileName)
	outputStore := common.DefaultStoreForPath(fileName)

	var output []byte

	switch cmdName {
	case "rotate", "add-master-keys", "remove-master-keys":
		var allKeys []keys.MasterKey
		kmsContext := kms.ParseKMSContext(sa.KMSC)

		for _, k := range kms.MasterKeysFromArnString(sa.KMS, kmsContext, sa.AWS) {
			allKeys = append(allKeys, k)
		}
		for _, k := range pgp.MasterKeysFromFingerprintString(sa.PGP) {
			allKeys = append(allKeys, k)
		}
		for _, k := range gcpkms.MasterKeysFromResourceIDString(sa.GCP) {
			allKeys = append(allKeys, k)
		}
		azKeys, err := azkv.MasterKeysFromURLs(sa.Azure)
		if err != nil {
			return err
		}
		for _, k := range azKeys {
			allKeys = append(allKeys, k)
		}

		var addKeys, rmKeys []keys.MasterKey

		switch cmdName {
		case "add-master-keys":
			addKeys = allKeys
		case "remove-master-keys":
			rmKeys = allKeys
		}

		output, err = rotate(rotateOpts{
			log:              log,
			OutputStore:      outputStore,
			InputStore:       inputStore,
			InputPath:        fileName,
			Cipher:           aes.NewCipher(),
			KeyServices:      ks,
			IgnoreMAC:        false,
			AddMasterKeys:    addKeys,
			RemoveMasterKeys: rmKeys,
		})
		if err != nil {
			return err
		}
		return os.WriteFile(fileName, output, 0o600)
	}

	if err != nil {
		return err
	}

	_, statErr := os.Stat(fileName)
	fileExists := statErr == nil

	opts := editOpts{
		log:            log,
		OutputStore:    outputStore,
		InputStore:     inputStore,
		InputPath:      fileName,
		Cipher:         aes.NewCipher(),
		KeyServices:    ks,
		IgnoreMAC:      false,
		ShowMasterKeys: false,
	}

	if fileExists {
		output, err = edit(opts)
	} else {
		// File doesn't exist, edit the example file instead
		var groups []sops.KeyGroup

		if groups, err = keyGroups(sa, fileName); err != nil {
			return err
		}

		if len(args) == 0 {
			return fmt.Errorf("no key management provider defined")
		}

		output, err = editExample(editExampleOpts{
			editOpts:          opts,
			UnencryptedSuffix: "",
			EncryptedSuffix:   "",
			EncryptedRegex:    "",
			KeyGroups:         groups,
			GroupThreshold:    0,
		})
	}

	if err != nil {
		return err
	}

	// We open the file *after* the operations on the tree have been
	// executed to avoid truncating it when there's errors
	file, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("could not open in-place file for writing: %s", err)
	}
	defer file.Close()
	if _, err = file.Write(output); err != nil {
		return err
	}
	log.Info("File written successfully")
	return nil
}

func keyGroups(sa SecretArgs, file string) ([]sops.KeyGroup, error) {
	var kmsKeys []keys.MasterKey
	var pgpKeys []keys.MasterKey
	var cloudKmsKeys []keys.MasterKey
	var azkvKeys []keys.MasterKey

	if sa.KMS != "" {
		kmsContext := kms.ParseKMSContext(sa.KMSC)
		for _, k := range kms.MasterKeysFromArnString(sa.KMS, kmsContext, sa.AWS) {
			kmsKeys = append(kmsKeys, k)
		}
	}
	if sa.GCP != "" {
		for _, k := range gcpkms.MasterKeysFromResourceIDString(sa.GCP) {
			cloudKmsKeys = append(cloudKmsKeys, k)
		}
	}
	if sa.Azure != "" {
		azureKeys, err := azkv.MasterKeysFromURLs(sa.Azure)
		if err != nil {
			return nil, err
		}
		for _, k := range azureKeys {
			azkvKeys = append(azkvKeys, k)
		}
	}
	if sa.PGP != "" {
		for _, k := range pgp.MasterKeysFromFingerprintString(sa.PGP) {
			pgpKeys = append(pgpKeys, k)
		}
	}

	var group sops.KeyGroup
	group = append(group, kmsKeys...)
	group = append(group, cloudKmsKeys...)
	group = append(group, azkvKeys...)
	group = append(group, pgpKeys...)
	return []sops.KeyGroup{group}, nil
}
