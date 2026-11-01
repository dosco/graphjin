package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/version"
)

const (
	backupManifestEntry     = "manifest.json"
	backupRolePasswordEntry = "database/role-password"
	backupDatabaseEntry     = "database/neko.dump"
	backupVolumeEntryPrefix = "volumes/"
	backupVolumeEntrySuffix = ".tar"
	backupManifestFormat    = 1
)

// backupVolumes are the named volumes holding durable state outside neko-db:
// the OpenNeko config (config.json, the at-rest secret key) and the customer
// GraphJin config, which also holds the Library files. The demo's
// AdventureWorks GraphJin config is included when that volume exists.
var backupVolumes = []struct {
	name     string
	required bool
}{
	{"openneko-config", true},
	{"openneko-graphjin-cli", true},
	{"graphjin-config", false},
}

var backupRolePassword = regexp.MustCompile(`^(SCRAM-SHA-256\$[A-Za-z0-9+/=:$]+|md5[0-9a-f]{32})$`)

type backupManifest struct {
	Format          int       `json:"format"`
	CreatedAt       time.Time `json:"created_at"`
	Project         string    `json:"project"`
	OpenNekoVersion string    `json:"openneko_version"`
	Database        struct {
		User          string `json:"user"`
		Name          string `json:"name"`
		ServerVersion string `json:"server_version"`
	} `json:"database"`
	Volumes []string `json:"volumes"`
}

func newBackupCmd() *cobra.Command {
	var destination string
	cmd := &cobra.Command{
		Use:   "backup [--to <file>]",
		Short: "Write an encrypted backup of the database and config volumes to a file",
		Long: `Write one encrypted file holding a pg_dump of neko-db plus the OpenNeko and
GraphJin config volumes (including Library files). The stack must be running.

By default the file goes to $XDG_DATA_HOME/openneko/backups/<installation>/<project>
(override with OPENNEKO_BACKUP_DIR). It is encrypted with this installation's
backup key; keep a copy of the key elsewhere with ` + "`openneko backup key export`" + `.
Restore with ` + "`openneko restore <file> --confirm RESTORE`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sup, _, project, files, err := currentStackCompose()
			if err != nil {
				return err
			}
			_, err = createBackup(cmd.Context(), sup, project, files, destination, cmd.OutOrStdout())
			return err
		},
	}
	cmd.Flags().StringVar(&destination, "to", "", "File (or existing directory) to write the backup to")
	cmd.AddCommand(newBackupKeyCmd())
	return cmd
}

func newBackupKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Export or adopt the key backups are encrypted with",
	}
	cmd.AddCommand(newBackupKeyAdoptCmd(), newBackupKeyExportCmd())
	return cmd
}

func newBackupKeyAdoptCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   "adopt --from <key-file>",
		Short: "Use an exported backup key for this installation",
		Long: `Install an exported backup key as this installation's key, for example after
moving to a new host. Refuses to replace a different existing key; a backup made
with another key can always be restored with ` + "`openneko restore --key-file`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if source == "" {
				return fmt.Errorf("--from is required")
			}
			absolute, err := filepath.Abs(source)
			if err != nil {
				return err
			}
			value, err := config.ReadBackupKey(absolute)
			if err != nil {
				return err
			}
			sup := compose.New(assets.ComposeFS)
			keyPath, explicit, err := backupKeyPath(sup)
			if err != nil {
				return err
			}
			if explicit {
				return fmt.Errorf("OPENNEKO_BACKUP_KEY_FILE is set to %s; unset it to adopt a key", keyPath)
			}
			existing, err := config.ReadBackupKey(keyPath)
			switch {
			case err == nil && existing == value:
				fmt.Fprintf(cmd.OutOrStdout(), "backup key %s is already this installation's key (%s)\n", backupKeyIDString(value), keyPath)
				return nil
			case err == nil:
				return fmt.Errorf("this installation already has a different backup key %s (%s); refusing to replace it. Restore older backups with `openneko restore <file> --key-file %s`", backupKeyIDString(existing), keyPath, absolute)
			case !errors.Is(err, fs.ErrNotExist):
				return err
			}
			if err := config.WriteBackupKey(keyPath, value); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "adopted backup key %s; stored at %s\n", backupKeyIDString(value), keyPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "from", "", "Exported backup key file")
	return cmd
}

func newBackupKeyExportCmd() *cobra.Command {
	var destination string
	cmd := &cobra.Command{
		Use:   "export --to <key-file>",
		Short: "Export a recovery copy of the backup encryption key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if destination == "" {
				return fmt.Errorf("--to is required")
			}
			value, _, err := loadBackupKey(compose.New(assets.ComposeFS), true)
			if err != nil {
				return err
			}
			absolute, err := filepath.Abs(destination)
			if err != nil {
				return err
			}
			if err := config.WriteBackupKey(absolute, value); err != nil {
				if errors.Is(err, fs.ErrExist) {
					return fmt.Errorf("refusing to overwrite existing file %s", absolute)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported backup key %s to %s; store it separately from the backups\n", backupKeyIDString(value), absolute)
			return nil
		},
	}
	cmd.Flags().StringVar(&destination, "to", "", "New file to receive the key (must not already exist)")
	return cmd
}

func currentStackCompose() (*compose.Supervisor, compose.Mode, string, []string, error) {
	sup := compose.New(assets.ComposeFS)
	project, err := sup.ProjectName("")
	if err != nil {
		return nil, "", "", nil, err
	}
	mode, ok := compose.ModeFromProjectName(project)
	if !ok {
		mode = compose.ModeProd
	}
	files, err := sup.Materialize(mode)
	if err != nil {
		return nil, "", "", nil, err
	}
	return sup, mode, project, files, nil
}

// resolveBackupDestination returns the final path for a new backup. An empty
// destination or an existing directory gets a timestamped file name.
func resolveBackupDestination(destination, defaultDir, project string, now time.Time) (string, error) {
	name := project + "-" + now.UTC().Format("20060102T150405Z") + backupFileSuffix
	if strings.TrimSpace(destination) == "" {
		return filepath.Join(defaultDir, name), nil
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(absolute); err == nil {
		if info.IsDir() {
			return filepath.Join(absolute, name), nil
		}
		return "", fmt.Errorf("refusing to overwrite existing file %s", absolute)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return absolute, nil
}

// createBackup writes a complete encrypted backup of the running stack and
// returns its path. The file appears under its final name only once every
// byte has been written and synced.
func createBackup(ctx context.Context, sup *compose.Supervisor, project string, files []string, destination string, out io.Writer) (string, error) {
	stack := backupStack{sup: sup, project: project, files: files}
	if running, err := stack.serviceRunning(ctx, "neko-db"); err != nil {
		return "", err
	} else if !running {
		return "", fmt.Errorf("neko-db is not running in %s; start the stack (`openneko start -d`) before taking a backup", project)
	}
	key, keyPath, err := loadBackupKey(sup, true)
	if err != nil {
		return "", err
	}
	defaultDir, err := backupDirectory(sup, project)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	target, err := resolveBackupDestination(destination, defaultDir, project, now)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}

	manifest := backupManifest{
		Format:          backupManifestFormat,
		CreatedAt:       now,
		Project:         project,
		OpenNekoVersion: version.Version,
	}
	identity, err := stack.psql(ctx, `select current_user || '|' || current_database() || '|' || current_setting('server_version')`)
	if err != nil {
		return "", fmt.Errorf("inspect neko-db: %w", err)
	}
	parts := strings.Split(identity, "|")
	if len(parts) != 3 {
		return "", fmt.Errorf("inspect neko-db: unexpected response %q", identity)
	}
	manifest.Database.User, manifest.Database.Name, manifest.Database.ServerVersion = parts[0], parts[1], parts[2]
	rolePassword, err := stack.psql(ctx, `select coalesce(rolpassword, '') from pg_authid where rolname = current_user`)
	if err != nil {
		return "", fmt.Errorf("read neko-db role: %w", err)
	}
	volumes := map[string]string{}
	for _, volume := range backupVolumes {
		name, err := stack.volumeName(ctx, volume.name)
		if err != nil {
			return "", err
		}
		if name == "" {
			if volume.required {
				return "", fmt.Errorf("volume %s of %s does not exist", volume.name, project)
			}
			continue
		}
		volumes[volume.name] = name
		manifest.Volumes = append(manifest.Volumes, volume.name)
	}
	image, err := stack.databaseImage(ctx)
	if err != nil {
		return "", err
	}

	partial, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".partial-*")
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = partial.Close()
			_ = os.Remove(partial.Name())
		}
	}()
	encrypter, err := newBackupEncrypter(partial, key)
	if err != nil {
		return "", err
	}
	bundle := newBackupBundleWriter(encrypter)
	writeEntry := func(name string, write func(io.Writer) error) error {
		entry, err := bundle.Entry(name)
		if err != nil {
			return err
		}
		if err := write(entry); err != nil {
			return err
		}
		return entry.Close()
	}

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeEntry(backupManifestEntry, func(w io.Writer) error {
		_, err := w.Write(manifestJSON)
		return err
	}); err != nil {
		return "", err
	}
	for _, volume := range manifest.Volumes {
		fmt.Fprintf(out, "Archiving volume %s...\n", volume)
		if err := writeEntry(backupVolumeEntryPrefix+volume+backupVolumeEntrySuffix, func(w io.Writer) error {
			return stack.archiveVolume(ctx, image, volumes[volume], w)
		}); err != nil {
			return "", fmt.Errorf("archive volume %s: %w", volume, err)
		}
	}
	if err := writeEntry(backupRolePasswordEntry, func(w io.Writer) error {
		_, err := io.WriteString(w, rolePassword)
		return err
	}); err != nil {
		return "", err
	}
	fmt.Fprintln(out, "Dumping neko-db...")
	if err := writeEntry(backupDatabaseEntry, func(w io.Writer) error {
		return stack.dumpDatabase(ctx, w)
	}); err != nil {
		return "", fmt.Errorf("dump neko-db: %w", err)
	}
	if err := bundle.Finish(); err != nil {
		return "", err
	}
	if err := encrypter.Close(); err != nil {
		return "", err
	}
	if err := partial.Sync(); err != nil {
		return "", err
	}
	if err := partial.Close(); err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("refusing to overwrite existing file %s", target)
	}
	if err := os.Rename(partial.Name(), target); err != nil {
		return "", err
	}
	committed = true
	syncDirectory(filepath.Dir(target))
	size := int64(0)
	if info, err := os.Stat(target); err == nil {
		size = info.Size()
	}
	fmt.Fprintf(out, "Backup written to %s (%s)\n", target, humanBytes(size))
	fmt.Fprintf(out, "Encrypted with backup key %s (%s); keep a copy of it elsewhere: `openneko backup key export --to <file>`\n", backupKeyIDString(key), keyPath)
	return target, nil
}

func syncDirectory(dir string) {
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
}

// backupStack runs the docker operations backup and restore need against one
// Compose project.
type backupStack struct {
	sup     *compose.Supervisor
	project string
	files   []string
}

func (s backupStack) compose(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	var stderr bytes.Buffer
	code, err := s.sup.RunIO(ctx, s.project, s.files, args, stdin, stdout, &stderr)
	if err != nil {
		return err
	}
	if code != 0 {
		return commandFailure(fmt.Sprintf("docker compose %s", strings.Join(args[:min(len(args), 3)], " ")), code, stderr.String())
	}
	return nil
}

func (s backupStack) composeOutput(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	if err := s.compose(ctx, nil, &stdout, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (s backupStack) serviceRunning(ctx context.Context, service string) (bool, error) {
	ids, err := s.composeOutput(ctx, "ps", "--status", "running", "-q", service)
	if err != nil {
		return false, err
	}
	return ids != "", nil
}

// databaseImage is the image of the project's neko-db container. It carries
// tar and runs as root, so it can archive and restore any volume without
// pulling another image.
func (s backupStack) databaseImage(ctx context.Context) (string, error) {
	ids, err := s.composeOutput(ctx, "ps", "-a", "-q", "neko-db")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(strings.SplitN(ids, "\n", 2)[0])
	if id == "" {
		return "", fmt.Errorf("no neko-db container exists in %s", s.project)
	}
	image, err := dockerOutput(ctx, "inspect", "--format", "{{.Image}}", id)
	if err != nil {
		return "", err
	}
	if image == "" {
		return "", fmt.Errorf("cannot determine the neko-db image")
	}
	return image, nil
}

// volumeName resolves a Compose volume to its Docker name by label, so
// volumes created under a custom name are found too. Empty means it does
// not exist.
func (s backupStack) volumeName(ctx context.Context, volume string) (string, error) {
	names, err := dockerOutput(ctx, "volume", "ls", "-q",
		"--filter", "label=com.docker.compose.project="+s.project,
		"--filter", "label=com.docker.compose.volume="+volume)
	if err != nil {
		return "", err
	}
	lines := uniqueLines(names)
	if len(lines) > 1 {
		return "", fmt.Errorf("found several Docker volumes for %s/%s: %s", s.project, volume, strings.Join(lines, ", "))
	}
	if len(lines) == 0 {
		return "", nil
	}
	return lines[0], nil
}

func (s backupStack) psql(ctx context.Context, query string) (string, error) {
	return s.composeOutput(ctx, "exec", "-T", "neko-db", "sh", "-ec",
		`exec psql -X -Atq -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"`,
		"openneko-psql", query)
}

func (s backupStack) dumpDatabase(ctx context.Context, w io.Writer) error {
	return s.compose(ctx, nil, w, "exec", "-T", "neko-db", "sh", "-ec",
		`exec pg_dump --format=custom --no-password -U "$POSTGRES_USER" -d "$POSTGRES_DB"`)
}

func (s backupStack) archiveVolume(ctx context.Context, image, volume string, w io.Writer) error {
	return dockerStream(ctx, nil, w, "run", "--rm", "--network", "none",
		"--user", "0:0", "--entrypoint", "tar",
		"--mount", "type=volume,src="+volume+",dst=/source,readonly",
		image, "--numeric-owner", "-C", "/source", "-cf", "-", ".")
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	if err := dockerStream(ctx, nil, &stdout, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func dockerStream(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return commandFailure("docker "+strings.Join(args[:min(len(args), 2)], " "), exitErr.ExitCode(), stderr.String())
		}
		return err
	}
	return nil
}

func commandFailure(command string, code int, stderr string) error {
	detail := strings.TrimSpace(stderr)
	if len(detail) > 1200 {
		detail = "..." + detail[len(detail)-1200:]
	}
	if detail == "" {
		return fmt.Errorf("%s exited with status %d", command, code)
	}
	return fmt.Errorf("%s exited with status %d: %s", command, code, detail)
}
