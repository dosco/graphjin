package secrets

import (
	"bufio"
	"bytes"
	"crypto/md5" // #nosec: G501
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/shlex"
	"go.mozilla.org/sops/v3"
	"go.mozilla.org/sops/v3/cmd/sops/codes"
	"go.mozilla.org/sops/v3/cmd/sops/common"
	"go.mozilla.org/sops/v3/keyservice"
	"go.mozilla.org/sops/v3/version"
	"go.uber.org/zap"
)

type editOpts struct {
	log            *zap.SugaredLogger
	Cipher         sops.Cipher
	InputStore     common.Store
	OutputStore    common.Store
	InputPath      string
	IgnoreMAC      bool
	KeyServices    []keyservice.KeyServiceClient
	ShowMasterKeys bool
}

type editExampleOpts struct {
	editOpts
	UnencryptedSuffix string
	EncryptedSuffix   string
	UnencryptedRegex  string
	EncryptedRegex    string
	KeyGroups         []sops.KeyGroup
	GroupThreshold    int
}

type runEditorUntilOkOpts struct {
	log            *zap.SugaredLogger
	TmpFile        *os.File
	OriginalHash   []byte
	InputStore     sops.Store
	ShowMasterKeys bool
	Tree           *sops.Tree
}

var fileBytes = `# Set your secrets envionment variables and save this file
GJ_DATABASE_PASSWORD: secret_db_password
GJ_ADMIN_SECRET_KEY: hotdeploy_admin_secret_key
GJ_SECRET_KEY: graphjin_generic_secret_key
GJ_AUTH_JWT_SECRET: jwt_auth_secret_key`

// editExample edits the example file
func editExample(opts editExampleOpts) ([]byte, error) {
	branches, err := opts.InputStore.LoadPlainFile([]byte(fileBytes))
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling file: %s", err)
	}
	path, err := filepath.Abs(opts.InputPath)
	if err != nil {
		return nil, err
	}
	tree := sops.Tree{
		Branches: branches,
		Metadata: sops.Metadata{
			KeyGroups:         opts.KeyGroups,
			UnencryptedSuffix: "",
			EncryptedSuffix:   "",
			EncryptedRegex:    opts.EncryptedRegex,
			Version:           version.Version,
			ShamirThreshold:   opts.GroupThreshold,
		},
		FilePath: path,
	}

	// Generate a data key
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		return nil, fmt.Errorf("error encrypting the data key with one or more master keys: %s", errs)
	}

	return editTree(opts.editOpts, &tree, dataKey)
}

// edit edits the file at the given path using options passed.
func edit(opts editOpts) ([]byte, error) {
	// Load the file
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
	// Decrypt the file
	dataKey, err := common.DecryptTree(common.DecryptTreeOpts{
		Cipher:      opts.Cipher,
		IgnoreMac:   opts.IgnoreMAC,
		Tree:        tree,
		KeyServices: opts.KeyServices,
	})
	if err != nil {
		return nil, err
	}

	return editTree(opts, tree, dataKey)
}

// editTree edits the tree using the options passed.
func editTree(opts editOpts, tree *sops.Tree, dataKey []byte) ([]byte, error) {
	// Create temporary file for editing
	tmpdir, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("could not create temporary directory: %s", err)
	}
	defer os.RemoveAll(tmpdir)

	tmpfile, err := os.Create(filepath.Join(tmpdir, filepath.Base(opts.InputPath)))
	if err != nil {
		return nil, fmt.Errorf("could not create temporary file: %s", err)
	}

	// Write to temporary file
	var out []byte
	if opts.ShowMasterKeys {
		out, err = opts.OutputStore.EmitEncryptedFile(*tree)
	} else {
		out, err = opts.OutputStore.EmitPlainFile(tree.Branches)
	}
	if err != nil {
		return nil, fmt.Errorf("could not marshal tree: %s", err)
	}
	_, err = tmpfile.Write(out)
	if err != nil {
		return nil, fmt.Errorf("could not write output file: %s", err)
	}

	// Close temporary file, since Windows won't delete the file unless it's closed beforehand
	defer tmpfile.Close()

	// Compute file hash to detect if the file has been edited
	origHash, err := hashFile(tmpfile.Name())
	if err != nil {
		return nil, fmt.Errorf("could not hash file: %s", err)
	}

	// Let the user edit the file
	err = runEditorUntilOk(runEditorUntilOkOpts{
		log:            opts.log,
		InputStore:     opts.InputStore,
		OriginalHash:   origHash,
		TmpFile:        tmpfile,
		ShowMasterKeys: opts.ShowMasterKeys,
		Tree:           tree,
	})

	if err != nil {
		return nil, err
	}

	// Encrypt the file
	err = common.EncryptTree(common.EncryptTreeOpts{
		DataKey: dataKey, Tree: tree, Cipher: opts.Cipher,
	})
	if err != nil {
		return nil, err
	}

	// Output the file
	encryptedFile, err := opts.OutputStore.EmitEncryptedFile(*tree)
	if err != nil {
		return nil, common.NewExitError(fmt.Sprintf("could not marshal tree: %s", err), codes.ErrorDumpingTree)
	}
	return encryptedFile, nil
}

// runEditorUntilOk runs the editor until the file is saved and the hash is different
func runEditorUntilOk(opts runEditorUntilOkOpts) error {
	for {
		err := runEditor(opts.TmpFile.Name())
		if err != nil {
			return fmt.Errorf("could not run editor: %s", err)
		}
		newHash, err := hashFile(opts.TmpFile.Name())
		if err != nil {
			return fmt.Errorf("could not hash file: %s", err)
		}
		if bytes.Equal(newHash, opts.OriginalHash) {
			return fmt.Errorf("file has not changed, exiting")
		}
		edited, err := os.ReadFile(opts.TmpFile.Name())
		if err != nil {
			return fmt.Errorf("could not read edited file: %s", err)
		}
		newBranches, err := opts.InputStore.LoadPlainFile(edited)
		if err != nil {
			opts.log.Errorf("could not load tree, probably due to invalid " +
				"syntax. Press a key to return to the editor, or Ctrl+C to " +
				"exit.")
			_, _ = bufio.NewReader(os.Stdin).ReadByte()
			continue
		}
		if opts.ShowMasterKeys {
			// The file is not actually encrypted, but it contains SOPS
			// metadata
			t, err := opts.InputStore.LoadEncryptedFile(edited)
			if err != nil {
				opts.log.Errorf("invalid metadata. Press a key to " +
					"return to the editor, or Ctrl+C to exit.")
				_, _ = bufio.NewReader(os.Stdin).ReadByte()
				continue
			}
			// Replace the whole tree, because otherwise newBranches would
			// contain the SOPS metadata
			opts.Tree = &t
		}
		opts.Tree.Branches = newBranches
		needVersionUpdated, err := version.AIsNewerThanB(version.Version, opts.Tree.Metadata.Version)
		if err != nil {
			return fmt.Errorf("failed to compare document version %q with program version %q: %v",
				opts.Tree.Metadata.Version, version.Version, err)
		}
		if needVersionUpdated {
			opts.Tree.Metadata.Version = version.Version
		}
		if opts.Tree.Metadata.MasterKeyCount() == 0 {
			opts.log.Error("no master keys were provided, so sops can't " +
				"encrypt the file. Press a key to return to the editor, or " +
				"Ctrl+C to exit.")
			_, _ = bufio.NewReader(os.Stdin).ReadByte()
			continue
		}
		break
	}
	return nil
}

// hashFile returns the MD5 hash of the file at the given path
func hashFile(filePath string) ([]byte, error) {
	var result []byte
	file, err := os.Open(filePath)
	if err != nil {
		return result, err
	}
	defer file.Close()
	hash := md5.New() // #nosec: G401
	if _, err := io.Copy(hash, file); err != nil {
		return result, err
	}
	return hash.Sum(result), nil
}

// runEditor runs the editor
func runEditor(path string) error {
	editor := os.Getenv("EDITOR")
	var cmd *exec.Cmd

	if editor == "" {
		editor, err := lookupAnyEditor("nvim", "vim", "vi", "nano", "pico")
		if err != nil {
			return err
		}
		cmd = exec.Command(editor, path)
	} else {
		parts, err := shlex.Split(editor)
		if err != nil {
			return fmt.Errorf("invalid $EDITOR: %s", editor)
		}
		parts = append(parts, path)
		cmd = exec.Command(parts[0], parts[1:]...) // #nosec: G204
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// lookupAnyEditor looks up the first available editor
func lookupAnyEditor(editorNames ...string) (editorPath string, err error) {
	for _, editorName := range editorNames {
		editorPath, err = exec.LookPath(editorName)
		if err == nil {
			return editorPath, nil
		}
	}
	return "", fmt.Errorf("no editor available: sops attempts to use the editor defined in the EDITOR environment variable, and if that's not set defaults to any of %s, but none of them could be found", strings.Join(editorNames, ", "))
}
