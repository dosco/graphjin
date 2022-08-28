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

type Args struct {
	KMS, KMSC, AWS, GCP, Azure, PGP string
}

func Run(cmdName, fileName string, sa Args, args []string, log *zap.SugaredLogger) error {
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

	// conf, err := loadConfig(cpath, fileName, nil)
	// if err != nil {
	// 	return err
	// }

	ks := []keyservice.KeyServiceClient{keyservice.NewLocalClient()}
	inputStore := common.DefaultStoreForPath(fileName)
	outputStore := common.DefaultStoreForPath(fileName)

	var output []byte

	switch cmdName {
	case "encrypt":
		/*
			var groups []sops.KeyGroup
			groups, err = keyGroups(sa, fileName)
			if err != nil {
				return toExitError(err)
			}
			output, err = encrypt(encryptOpts{
				OutputStore:       outputStore,
				InputStore:        inputStore,
				InputPath:         fileName,
				Cipher:            aes.NewCipher(),
				UnencryptedSuffix: "",
				EncryptedSuffix:   "",
				EncryptedRegex:    "",
				KeyServices:       ks,
				KeyGroups:         groups,
				GroupThreshold:    0,
			})
		*/

	case "decrypt":
		/*
			output, err = decrypt(decryptOpts{
				OutputStore: outputStore,
				InputStore:  inputStore,
				InputPath:   fileName,
				Cipher:      aes.NewCipher(),
				Extract:     nil,
				KeyServices: ks,
				IgnoreMAC:   false,
			})
		*/

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
		return os.WriteFile(fileName, output, 0600)
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

/*
func parseTreePath(arg string) ([]interface{}, error) {
	var path []interface{}

	components := strings.Split(arg, "[")
	for _, component := range components {
		if component == "" {
			continue
		}
		if component[len(component)-1] != ']' {
			return nil, fmt.Errorf("component %s doesn't end with ]", component)
		}
		component = component[:len(component)-1]
		if component[0] == byte('"') || component[0] == byte('\'') {
			// The component is a string
			component = component[1 : len(component)-1]
			path = append(path, component)
		} else {
			// The component must be a number
			i, err := strconv.Atoi(component)
			if err != nil {
				return nil, err
			}
			path = append(path, i)
		}
	}
	return path, nil
}
*/

func keyGroups(sa Args, file string) ([]sops.KeyGroup, error) {
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

	/*
		if sa.KMS == "" && sa.PGP == "" && sa.GCP == "" && sa.Azure == "" {
			kmsContext := kms.ParseKMSContext(sa.KMSC)
			conf, err := loadConfig("", file, kmsContext)
			// config file might just not be supplied, without any error
			if conf == nil {
				errMsg := "config file not found and no keys provided through command line options"
				if err != nil {
					errMsg = fmt.Sprintf("%s: %s", errMsg, err)
				}
				return nil, fmt.Errorf(errMsg)
			}
			return conf.KeyGroups, err
		}
	*/

	var group sops.KeyGroup
	group = append(group, kmsKeys...)
	group = append(group, cloudKmsKeys...)
	group = append(group, azkvKeys...)
	group = append(group, pgpKeys...)
	return []sops.KeyGroup{group}, nil
}

// loadConfig will look for an existing config file, either provided through the command line, or using config.FindConfigFile.
// Since a config file is not required, this function does not error when one is not found, and instead returns a nil config pointer
/*
func loadConfig(path, file string, kmsContext map[string]*string) (*config.Config, error) {
	var err error

	// if path == "" {
	// 	if path, err = config.FindConfigFile("."); err != nil {
	// 		return nil, nil
	// 	}
	// }

	conf, err := config.LoadCreationRuleForFile(path, file, kmsContext)
	if err != nil {
		return nil, err
	}
	return conf, nil
}
*/
