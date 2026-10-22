package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
)

// backupDirectory is where `openneko backup` writes by default and where
// doctor looks for the latest backup: OPENNEKO_BACKUP_DIR, or
// $XDG_DATA_HOME/openneko/backups/<installation>/<project>. The installation
// identity keeps two checkouts that use the same project name apart. The
// directory is not created here.
func backupDirectory(sup *compose.Supervisor, project string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("OPENNEKO_BACKUP_DIR")); override != "" {
		return filepath.Abs(override)
	}
	project = strings.TrimSpace(project)
	if project == "" || strings.ContainsAny(project, `/\`) || project == "." || project == ".." {
		return "", fmt.Errorf("invalid OpenNeko project name %q", project)
	}
	installationID, err := sup.InstallationID()
	if err != nil {
		return "", err
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Abs(filepath.Join(base, "openneko", "backups", installationID, project))
}

// backupKeyPath returns the key file backups are encrypted with:
// OPENNEKO_BACKUP_KEY_FILE when set (it must already exist), otherwise a
// per-installation key in the host state directory, away from the backups
// themselves. explicit reports whether the path came from the environment.
func backupKeyPath(sup *compose.Supervisor) (path string, explicit bool, err error) {
	if override := strings.TrimSpace(os.Getenv("OPENNEKO_BACKUP_KEY_FILE")); override != "" {
		path, err := filepath.Abs(override)
		return path, true, err
	}
	installationID, err := sup.InstallationID()
	if err != nil {
		return "", false, err
	}
	stateHome, err := instance.StateHome()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(stateHome, "openneko", "backup-keys", "installation-"+installationID+".key"), false, nil
}

// loadBackupKey reads the backup key. With create, a missing default key is
// generated; an explicit OPENNEKO_BACKUP_KEY_FILE is never created implicitly.
func loadBackupKey(sup *compose.Supervisor, create bool) (string, string, error) {
	path, explicit, err := backupKeyPath(sup)
	if err != nil {
		return "", "", err
	}
	if create && !explicit {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", "", err
		}
		value, err := config.CreateBackupKey(path)
		return value, path, err
	}
	value, err := config.ReadBackupKey(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", path, fmt.Errorf("backup key %s does not exist; pass --key-file with the key the backup was made with, or adopt it with `openneko backup key adopt --from <key-file>`", path)
	}
	return value, path, err
}

type backupFileInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// latestBackup returns the most recent complete backup file in dir. A missing
// directory simply means there are no backups yet.
func latestBackup(dir string) (*backupFileInfo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []backupFileInfo
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, backupFileSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, backupFileInfo{Path: filepath.Join(dir, name), ModTime: info.ModTime(), Size: info.Size()})
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].ModTime.Equal(found[j].ModTime) {
			return found[i].Path > found[j].Path
		}
		return found[i].ModTime.After(found[j].ModTime)
	})
	return &found[0], nil
}

// backupDoctorLine summarizes backup protection for doctor. Having no backup,
// or an old one, is a warning rather than a failure.
func backupDoctorLine(latest *backupFileInfo, dir string, now time.Time) string {
	if latest == nil {
		return fmt.Sprintf("warning — no backup found in %s; run `openneko backup`", dir)
	}
	age := now.Sub(latest.ModTime)
	if age > 7*24*time.Hour {
		return fmt.Sprintf("warning — latest backup is %s old (%s); run `openneko backup`", durationAge(age), latest.Path)
	}
	return fmt.Sprintf("latest %s (%s old, %s)", latest.Path, durationAge(age), humanBytes(latest.Size))
}

func durationAge(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	return value.Round(time.Minute).String()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// backupsKeptNote tells an operator who just removed data volumes where
// their backup files still are.
func backupsKeptNote(sup *compose.Supervisor, project string) string {
	dir, err := backupDirectory(sup, project)
	if err != nil {
		return "backup files written by `openneko backup` were not touched"
	}
	return fmt.Sprintf("backup files in %s were not touched; restore one with `openneko restore <file> --confirm RESTORE`", dir)
}
