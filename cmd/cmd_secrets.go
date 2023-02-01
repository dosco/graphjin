package main

import (
	"fmt"
	"path/filepath"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var sopsHelp = `GraphJin Secrets Management (Using Mozilla SOPS)

To encrypt or decrypt a document with AWS KMS, specify the KMS ARN
in the --aws flag or in the SOPS_KMS_ARN environment variable.
(you need valid credentials in ~/.aws/credentials or in your env)

To encrypt or decrypt a document with GCP KMS, specify the
GCP KMS resource ID in the --gcp-kms flag or in the SOPS_GCP_KMS_IDS
environment variable.
(you need to setup google application default credentials. See
https://developers.google.com/identity/protocols/application-default-credentials)

To encrypt or decrypt a document with Azure Key Vault, specify the
Azure Key Vault key URL in the --azure-kv flag or in the SOPS_AZURE_KEYVAULT_URL
environment variable.
(authentication is based on environment variables, see
https://docs.microsoft.com/en-us/go/azure/azure-sdk-go-authorization#use-environment-based-authentication.
The user/sp needs the key/encrypt and key/decrypt permissions)

To manage master keys in existing documents, use "add-master-keys" or 
"remove-master-keys" with the "{kms,pgp,gcp-kms,azure-kv}" flags.

To use a different GPG binary than the one in your PATH, set SOPS_GPG_EXEC.
To use a GPG key server other than gpg.mozilla.org, set SOPS_GPG_KEYSERVER.

To select a different editor than the default (vim), set EDITOR.

For more information, see the README at github.com/mozilla/sops`

func cmdSecrets() *cobra.Command {
	c := &cobra.Command{
		Use:   "secrets [options]",
		Short: "Secure key managament (AWS KMS, GCP KMS, Azure Key Vault & GPG)",
		Long:  sopsHelp,
	}

	rot := &cobra.Command{
		Use:   "rotate",
		Short: "Generate a new data encryption key and reencrypt all values with the new key",
	}
	c.AddCommand(rot)

	addMasterKeys := &cobra.Command{
		Use:   "add-master-keys",
		Short: "Rotate keys and add the provided comma-separated list to the list of master keys on the given file",
	}
	c.AddCommand(addMasterKeys)

	delMasterKeys := &cobra.Command{
		Use:   "remove-master-keys",
		Short: "Rotate keys and remove the provided comma-separated list from the list of master keys on the given file",
	}
	c.AddCommand(delMasterKeys)

	var sa serv.SecretArgs
	var secretsFile string

	c.PersistentFlags().StringVar(&secretsFile, "secrets-file", "", "Path to the secrets file")
	c.PersistentFlags().StringVar(&sa.KMS, "kms", "", "Comma separated list of KMS ARNs")
	c.PersistentFlags().StringVar(&sa.KMSC, "kms-context", "", "Comma separated list of KMS encryption context key:value pairs")
	c.PersistentFlags().StringVar(&sa.AWS, "aws-profile", "", "The AWS profile to use for requests to AWS")
	c.PersistentFlags().StringVar(&sa.GCP, "gcp-kms", "", "Comma separated list of GCP KMS resource IDs")
	c.PersistentFlags().StringVar(&sa.Azure, "azure-kv", "", "Comma separated list of Azure Key Vault URLs")
	c.PersistentFlags().StringVar(&sa.PGP, "pgp", "", "Comma separated list of PGP fingerprints")

	c.RunE = func(cmd *cobra.Command, args []string) error {
		var fileName string
		var err error

		if secretsFile != "" {
			fileName, err = filepath.Abs(secretsFile)
		} else {
			setup(cpath)
			if conf.SecretsFile != "" {
				fileName, err = filepath.Abs(conf.RelPath(conf.SecretsFile))
			}
		}

		if err != nil {
			return err
		}

		if fileName == "" {
			return fmt.Errorf("no secrets_file defined in the config or specified using the --secrets-file flag")
		}

		return serv.SecretsCmd(cmd.Name(), fileName, sa, args, log)
	}
	return c
}
