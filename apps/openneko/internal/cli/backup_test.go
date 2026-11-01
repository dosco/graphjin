package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
)

func configureBackupTestEnvironment(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("OPENNEKO_WORKSPACE_ROOT", root)
	t.Setenv("OPENNEKO_BACKUP_DIR", "")
	t.Setenv("OPENNEKO_BACKUP_KEY_FILE", "")
	return root
}

func TestValidateRestoreArgs(t *testing.T) {
	for _, test := range []struct {
		args    []string
		confirm string
		want    string
	}{
		{nil, "RESTORE", "exactly one backup file"},
		{[]string{"a", "b"}, "RESTORE", "exactly one backup file"},
		{[]string{" "}, "RESTORE", "exactly one backup file"},
		{[]string{"backup.openneko-backup"}, "", "--confirm RESTORE"},
		{[]string{"backup.openneko-backup"}, "restore", "--confirm RESTORE"},
		{[]string{"backup.openneko-backup"}, " RESTORE", "--confirm RESTORE"},
	} {
		if _, err := validateRestoreArgs(test.args, test.confirm); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("validateRestoreArgs(%q, %q) = %v, want %q", test.args, test.confirm, err, test.want)
		}
	}
	got, err := validateRestoreArgs([]string{"backup.openneko-backup"}, "RESTORE")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != "backup.openneko-backup" {
		t.Fatalf("restore path = %q", got)
	}
}

func TestRestoreCommandRefusesWithoutConfirmationBeforeTouchingAnything(t *testing.T) {
	configureBackupTestEnvironment(t)
	cmd := newRestoreCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{filepath.Join(t.TempDir(), "missing"+backupFileSuffix)})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--confirm RESTORE") {
		t.Fatalf("restore without confirmation = %v", err)
	}
}

func TestRestoreCommandRejectsBadFileWithoutTouchingStack(t *testing.T) {
	configureBackupTestEnvironment(t)
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := config.WriteBackupKey(keyFile, testBackupKey); err != nil {
		t.Fatal(err)
	}
	bogus := filepath.Join(t.TempDir(), "bogus"+backupFileSuffix)
	if err := os.WriteFile(bogus, []byte("not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newRestoreCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{bogus, "--confirm", "RESTORE", "--key-file", keyFile})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("restore of a bogus file = %v", err)
	}
	if strings.Contains(output.String(), "Stopping") {
		t.Fatal("restore touched the stack before verification")
	}

	good := writeTestBackupFile(t, otherBackupKey, validTestBackupContents(t))
	cmd = newRestoreCmd()
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{good, "--confirm", "RESTORE", "--key-file", keyFile})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--key-file") || !strings.Contains(err.Error(), backupKeyIDString(otherBackupKey)) {
		t.Fatalf("restore with the wrong key = %v", err)
	}
}

func TestBackupCommandRejectsArguments(t *testing.T) {
	cmd := newBackupCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"now"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("backup accepted a positional argument")
	}
}

func TestResolveBackupDestination(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 8, 7, 0, time.UTC)
	defaultDir := filepath.Join(t.TempDir(), "backups")
	got, err := resolveBackupDestination("", defaultDir, "openneko-prod", now)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(defaultDir, "openneko-prod-20261022T090807Z"+backupFileSuffix); got != want {
		t.Fatalf("default destination = %q, want %q", got, want)
	}
	dir := t.TempDir()
	if got, err := resolveBackupDestination(dir, defaultDir, "openneko-prod", now); err != nil || filepath.Dir(got) != dir {
		t.Fatalf("directory destination = %q, %v", got, err)
	}
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveBackupDestination(existing, defaultDir, "openneko-prod", now); err == nil {
		t.Fatal("backup would overwrite an existing file")
	}
	fresh := filepath.Join(dir, "new.backup")
	if got, err := resolveBackupDestination(fresh, defaultDir, "openneko-prod", now); err != nil || got != fresh {
		t.Fatalf("file destination = %q, %v", got, err)
	}
}

func TestBackupDirectoryIsInstallationScopedAndOverridable(t *testing.T) {
	root := configureBackupTestEnvironment(t)
	sup := compose.New(assets.ComposeFS)
	installationID, err := sup.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	got, err := backupDirectory(sup, "openneko-prod")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "data", "openneko", "backups", installationID, "openneko-prod"); got != want {
		t.Fatalf("backup directory = %q, want %q", got, want)
	}
	if _, err := backupDirectory(sup, "../escape"); err == nil {
		t.Fatal("backup directory accepted a path-like project name")
	}
	override := filepath.Join(root, "nas")
	t.Setenv("OPENNEKO_BACKUP_DIR", override)
	if got, err := backupDirectory(sup, "openneko-prod"); err != nil || got != override {
		t.Fatalf("override directory = %q, %v", got, err)
	}
}

func TestBackupKeyIsCreatedOnceAndExplicitKeyIsNeverCreated(t *testing.T) {
	root := configureBackupTestEnvironment(t)
	sup := compose.New(assets.ComposeFS)
	if _, _, err := loadBackupKey(sup, false); err == nil || !strings.Contains(err.Error(), "--key-file") {
		t.Fatalf("missing key error = %v", err)
	}
	first, path, err := loadBackupKey(sup, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, filepath.Join(root, "state", "openneko", "backup-keys")) {
		t.Fatalf("key path = %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v", info.Mode().Perm())
	}
	second, _, err := loadBackupKey(sup, true)
	if err != nil || second != first {
		t.Fatalf("key was not stable: %v", err)
	}

	explicit := filepath.Join(root, "explicit.key")
	t.Setenv("OPENNEKO_BACKUP_KEY_FILE", explicit)
	if _, _, err := loadBackupKey(sup, true); err == nil {
		t.Fatal("explicit key file was created implicitly")
	}
	if _, err := os.Stat(explicit); !os.IsNotExist(err) {
		t.Fatalf("explicit key file exists: %v", err)
	}
}

func TestBackupKeyExportAndAdopt(t *testing.T) {
	configureBackupTestEnvironment(t)
	destination := filepath.Join(t.TempDir(), "recovery", "backup.key")
	cmd := newBackupKeyExportCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--to", destination})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	exported, err := config.ReadBackupKey(destination)
	if err != nil {
		t.Fatal(err)
	}
	cmd = newBackupKeyExportCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--to", destination})
	if err := cmd.Execute(); err == nil {
		t.Fatal("export overwrote an existing file")
	}

	// Adopting the same key is a no-op; a different one is refused.
	var output bytes.Buffer
	cmd = newBackupKeyAdoptCmd()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--from", destination})
	if err := cmd.Execute(); err != nil || !strings.Contains(output.String(), "already") {
		t.Fatalf("adopt same key = %v, %q", err, output.String())
	}
	other := filepath.Join(t.TempDir(), "other.key")
	if err := config.WriteBackupKey(other, otherBackupKey); err != nil {
		t.Fatal(err)
	}
	cmd = newBackupKeyAdoptCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--from", other})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("adopt different key = %v", err)
	}
	current, _, err := loadBackupKey(compose.New(assets.ComposeFS), false)
	if err != nil || current != exported {
		t.Fatalf("installation key changed: %v", err)
	}

	// A fresh installation adopts the exported key.
	configureBackupTestEnvironment(t)
	cmd = newBackupKeyAdoptCmd()
	output.Reset()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--from", destination})
	if err := cmd.Execute(); err != nil || !strings.Contains(output.String(), "adopted backup key") {
		t.Fatalf("adopt on fresh installation = %v, %q", err, output.String())
	}
	adopted, _, err := loadBackupKey(compose.New(assets.ComposeFS), false)
	if err != nil || adopted != exported {
		t.Fatalf("adopted key differs: %v", err)
	}
}

func TestLatestBackupAndDoctorLine(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "backups")
	latest, err := latestBackup(dir)
	if err != nil || latest != nil {
		t.Fatalf("missing directory = %+v, %v", latest, err)
	}
	if line := backupDoctorLine(nil, dir, now); !strings.HasPrefix(line, "warning — no backup found") {
		t.Fatalf("no backup line = %q", line)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, age := range map[string]time.Duration{
		"older" + backupFileSuffix:                 48 * time.Hour,
		"newer" + backupFileSuffix:                 2 * time.Hour,
		".partial" + backupFileSuffix:              time.Minute,
		"unrelated.txt":                            time.Minute,
		".newer.openneko-backup.partial-123456789": time.Minute,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	latest, err = latestBackup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || filepath.Base(latest.Path) != "newer"+backupFileSuffix {
		t.Fatalf("latest backup = %+v", latest)
	}
	if line := backupDoctorLine(latest, dir, now); !strings.HasPrefix(line, "latest ") || !strings.Contains(line, "2h0m0s old") {
		t.Fatalf("fresh backup line = %q", line)
	}
	latest.ModTime = now.Add(-8 * 24 * time.Hour)
	if line := backupDoctorLine(latest, dir, now); !strings.HasPrefix(line, "warning — latest backup is") {
		t.Fatalf("stale backup line = %q", line)
	}
}

func TestUpgradeHasSkipBackupFlag(t *testing.T) {
	if newUpgradeCmd().Flags().Lookup("skip-backup") == nil {
		t.Fatal("upgrade is missing --skip-backup")
	}
}
