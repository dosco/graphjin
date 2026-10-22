package cli

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/config"
)

func newRestoreCmd() *cobra.Command {
	var confirmation string
	var keyFile string
	cmd := &cobra.Command{
		Use:   "restore <file> --confirm RESTORE",
		Short: "Restore the database and config volumes from an `openneko backup` file",
		Long: `Decrypt and verify the whole backup file first, then stop the stack, replace
the OpenNeko and GraphJin config volumes, recreate the neko database from the
dump, and start the stack again (migrations bring an older backup forward).
This overwrites live data and configuration.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := validateRestoreArgs(args, confirmation)
			if err != nil {
				return err
			}
			return runRestore(cmd.Context(), cmd, path, keyFile)
		},
	}
	cmd.Flags().StringVar(&confirmation, "confirm", "", "type RESTORE to allow destructive overwrite")
	cmd.Flags().StringVar(&keyFile, "key-file", "", "Backup key to decrypt with (default: this installation's key)")
	return cmd
}

func validateRestoreArgs(args []string, confirmation string) (string, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("restore takes exactly one backup file: openneko restore <file> --confirm RESTORE")
	}
	if err := validateRestoreConfirmation(confirmation); err != nil {
		return "", err
	}
	return filepath.Abs(args[0])
}

func validateRestoreConfirmation(raw string) error {
	if raw != "RESTORE" {
		return fmt.Errorf("restore overwrites live data and configuration; pass --confirm RESTORE exactly")
	}
	return nil
}

// verifiedBackup is what a full verification pass learned about a file.
type verifiedBackup struct {
	Manifest backupManifest
}

// verifyBackupFile decrypts and checks the whole file without touching the
// stack: every chunk must authenticate, entries must appear in the order
// backup writes them, each volume must be a readable tar archive and the
// dump must be a pg_dump custom-format archive.
func verifyBackupFile(path, key string) (*verifiedBackup, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	plain, err := newBackupDecrypter(file, key)
	if err != nil {
		return nil, err
	}
	bundle := newBackupBundleReader(plain)
	var result verifiedBackup
	expected := []string{backupManifestEntry}
	index := 0
	for {
		name, entry, err := bundle.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if index >= len(expected) || name != expected[index] {
			return nil, fmt.Errorf("unexpected entry %q in backup", name)
		}
		index++
		switch {
		case name == backupManifestEntry:
			raw, err := io.ReadAll(io.LimitReader(entry, 1<<20))
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &result.Manifest); err != nil {
				return nil, fmt.Errorf("invalid backup manifest: %w", err)
			}
			if result.Manifest.Format != backupManifestFormat {
				return nil, fmt.Errorf("unsupported backup manifest format %d", result.Manifest.Format)
			}
			for _, volume := range result.Manifest.Volumes {
				if !isBackupVolume(volume) {
					return nil, fmt.Errorf("backup contains unknown volume %q", volume)
				}
				expected = append(expected, backupVolumeEntryPrefix+volume+backupVolumeEntrySuffix)
			}
			expected = append(expected, backupRolePasswordEntry, backupDatabaseEntry)
		case strings.HasPrefix(name, backupVolumeEntryPrefix):
			if err := verifyTarStream(entry); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
		case name == backupRolePasswordEntry:
			raw, err := io.ReadAll(io.LimitReader(entry, 4096))
			if err != nil {
				return nil, err
			}
			if value := string(raw); value != "" && !backupRolePassword.MatchString(value) {
				return nil, fmt.Errorf("backup role password has an unexpected format")
			}
		case name == backupDatabaseEntry:
			magic := make([]byte, 5)
			if _, err := io.ReadFull(entry, magic); err != nil || string(magic) != "PGDMP" {
				return nil, fmt.Errorf("backup database dump is not a pg_dump custom-format archive")
			}
			if _, err := io.Copy(io.Discard, entry); err != nil {
				return nil, err
			}
		}
	}
	if index != len(expected) {
		return nil, fmt.Errorf("backup is incomplete: missing %s", expected[index])
	}
	return &result, nil
}

func isBackupVolume(name string) bool {
	for _, volume := range backupVolumes {
		if volume.name == name {
			return true
		}
	}
	return false
}

func verifyTarStream(r io.Reader) error {
	archive := tar.NewReader(r)
	for {
		_, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid volume archive: %w", err)
		}
		if _, err := io.Copy(io.Discard, archive); err != nil {
			return fmt.Errorf("invalid volume archive: %w", err)
		}
	}
	_, err := io.Copy(io.Discard, r)
	return err
}

func readRestoreKey(keyFile string) (string, error) {
	if keyFile != "" {
		absolute, err := filepath.Abs(keyFile)
		if err != nil {
			return "", err
		}
		return config.ReadBackupKey(absolute)
	}
	sup, _, _, _, err := currentStackCompose()
	if err != nil {
		return "", err
	}
	key, _, err := loadBackupKey(sup, false)
	return key, err
}

func runRestore(ctx context.Context, cmd *cobra.Command, path, keyFile string) error {
	out := cmd.OutOrStdout()
	key, err := readRestoreKey(keyFile)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Verifying %s...\n", path)
	verified, err := verifyBackupFile(path, key)
	if err != nil {
		var mismatch *backupKeyMismatchError
		if errors.As(err, &mismatch) {
			return fmt.Errorf("%w; pass --key-file with the key that made it (nothing was changed)", err)
		}
		return fmt.Errorf("backup verification failed; nothing was changed: %w", err)
	}
	manifest := verified.Manifest
	fmt.Fprintf(out, "Backup of %s taken %s by openneko %s verified.\n",
		manifest.Project, manifest.CreatedAt.Format("2006-01-02 15:04:05 UTC"), manifest.OpenNekoVersion)

	sup, _, project, files, err := currentStackCompose()
	if err != nil {
		return err
	}
	stack := backupStack{sup: sup, project: project, files: files}

	fmt.Fprintln(out, "Stopping the stack...")
	if err := stack.compose(ctx, nil, os.Stdout, "stop"); err != nil {
		return err
	}
	// Creating (not starting) every container makes sure the project's
	// volumes exist with Compose's labels, e.g. after `openneko reset`.
	if err := stack.compose(ctx, nil, os.Stdout, "up", "--no-start"); err != nil {
		return fmt.Errorf("prepare stack volumes: %w", err)
	}
	image, err := stack.databaseImage(ctx)
	if err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	plain, err := newBackupDecrypter(file, key)
	if err != nil {
		return err
	}
	failed := func(step string, err error) error {
		return fmt.Errorf("restore failed while %s; the stack is stopped and may hold partially restored data. Re-run the restore once the cause is fixed: %w", step, err)
	}
	bundle := newBackupBundleReader(plain)
	rolePassword := ""
	databaseRestored := false
	for {
		name, entry, err := bundle.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return failed("reading the backup", err)
		}
		switch {
		case strings.HasPrefix(name, backupVolumeEntryPrefix):
			volume := strings.TrimSuffix(strings.TrimPrefix(name, backupVolumeEntryPrefix), backupVolumeEntrySuffix)
			dockerName, err := stack.volumeName(ctx, volume)
			if err != nil {
				return failed("locating volume "+volume, err)
			}
			if dockerName == "" {
				fmt.Fprintf(out, "Skipping volume %s: %s does not use it.\n", volume, project)
				continue
			}
			fmt.Fprintf(out, "Restoring volume %s...\n", volume)
			if err := stack.restoreVolume(ctx, image, dockerName, entry); err != nil {
				return failed("restoring volume "+volume, err)
			}
		case name == backupRolePasswordEntry:
			raw, err := io.ReadAll(io.LimitReader(entry, 4096))
			if err != nil {
				return failed("reading the backup", err)
			}
			rolePassword = string(raw)
		case name == backupDatabaseEntry:
			fmt.Fprintln(out, "Restoring neko-db...")
			if err := stack.compose(ctx, nil, os.Stdout, "up", "-d", "--wait", "--wait-timeout", "180", "--no-deps", "neko-db"); err != nil {
				return failed("starting neko-db", err)
			}
			if err := stack.restoreDatabase(ctx, entry, rolePassword); err != nil {
				return failed("restoring neko-db", err)
			}
			databaseRestored = true
		}
	}
	if !databaseRestored {
		return failed("reading the backup", errors.New("the database dump is missing"))
	}

	fmt.Fprintln(out, "Starting the stack...")
	if err := stack.compose(ctx, nil, os.Stdout, "up", "-d", "--wait", "--wait-timeout", "180"); err != nil {
		return fmt.Errorf("restore completed but the stack failed to start: %w", err)
	}
	fmt.Fprintf(out, "Restore complete from %s.\n", path)
	return nil
}

func (s backupStack) restoreVolume(ctx context.Context, image, volume string, archive io.Reader) error {
	return dockerStream(ctx, bufio.NewReaderSize(archive, 1<<20), io.Discard, "run", "--rm", "-i", "--network", "none",
		"--user", "0:0", "--entrypoint", "sh",
		"--mount", "type=volume,src="+volume+",dst=/target",
		image, "-ec", `find /target -mindepth 1 -delete
exec tar --numeric-owner -xpf - -C /target`)
}

// restoreDatabase replaces the neko database with the dump: the database is
// dropped and recreated empty, then pg_restore runs in a single transaction.
// The role's password verifier from the backup is reinstated so it matches
// the restored config volume.
func (s backupStack) restoreDatabase(ctx context.Context, dump io.Reader, rolePassword string) error {
	recreate := `psql -X -q -v ON_ERROR_STOP=1 -v db="$POSTGRES_DB" -v owner="$POSTGRES_USER" -U "$POSTGRES_USER" -d postgres <<'SQL'
DROP DATABASE IF EXISTS :"db" WITH (FORCE);
CREATE DATABASE :"db" OWNER :"owner";
SQL`
	if err := s.compose(ctx, nil, io.Discard, "exec", "-T", "neko-db", "sh", "-ec", recreate); err != nil {
		return err
	}
	if err := s.compose(ctx, bufio.NewReaderSize(dump, 1<<20), io.Discard, "exec", "-T", "neko-db", "sh", "-ec",
		`exec pg_restore --clean --if-exists --exit-on-error --single-transaction --no-password -U "$POSTGRES_USER" -d "$POSTGRES_DB"`); err != nil {
		return err
	}
	if rolePassword == "" {
		return nil
	}
	if !backupRolePassword.MatchString(rolePassword) {
		return fmt.Errorf("backup role password has an unexpected format")
	}
	_, err := s.psql(ctx, "ALTER ROLE CURRENT_USER PASSWORD '"+rolePassword+"'")
	return err
}
