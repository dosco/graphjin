package migrate

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
)

var migrationPattern = regexp.MustCompile(`\A(\d+)_[^\.]+\.sql\z`)

var ErrNoFwMigration = errors.Errorf("no sql in forward migration step")

type BadVersionError string

func (e BadVersionError) Error() string {
	return string(e)
}

type IrreversibleMigrationError struct {
	m *Migration
}

func (e IrreversibleMigrationError) Error() string {
	return fmt.Sprintf("Irreversible migration: %d - %s", e.m.Sequence, e.m.Name)
}

type NoMigrationsFoundError struct {
	Path string
}

func (e NoMigrationsFoundError) Error() string {
	return fmt.Sprintf("No migrations found at %s", e.Path)
}

type MigrationPgError struct {
	Sql   string
	Error error
}

type Migration struct {
	Sequence int32
	Name     string
	UpSQL    string
	DownSQL  string
}

type MigratorOptions struct {
	// DisableTx causes the Migrator not to run migrations in a transaction.
	DisableTx bool
	// MigratorFS is the interface used for collecting the migrations.
	MigratorFS MigratorFS
}

type Migrator struct {
	conn         *pgx.Conn
	versionTable string
	options      *MigratorOptions
	Migrations   []*Migration
	OnStart      func(int32, string, string, string) // OnStart is called when a migration is run with the sequence, name, direction, and SQL
	Data         map[string]interface{}              // Data available to use in migrations
}

func NewMigrator(conn *pgx.Conn, versionTable string) (m *Migrator, err error) {
	return NewMigratorEx(conn, versionTable, &MigratorOptions{MigratorFS: defaultMigratorFS{}})
}

func NewMigratorEx(conn *pgx.Conn, versionTable string, opts *MigratorOptions) (m *Migrator, err error) {
	m = &Migrator{conn: conn, versionTable: versionTable, options: opts}
	err = m.ensureSchemaVersionTableExists()
	m.Migrations = make([]*Migration, 0)
	m.Data = make(map[string]interface{})
	return
}

type MigratorFS interface {
	ReadDir(dirname string) ([]os.FileInfo, error)
	ReadFile(filename string) ([]byte, error)
	Glob(pattern string) (matches []string, err error)
}

type defaultMigratorFS struct{}

func (defaultMigratorFS) ReadDir(dirname string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(dirname)
}

func (defaultMigratorFS) ReadFile(filename string) ([]byte, error) {
	return ioutil.ReadFile(filename)
}

func (defaultMigratorFS) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func FindMigrationsEx(path string, fs MigratorFS) ([]string, error) {
	path = strings.TrimRight(path, string(filepath.Separator))

	fileInfos, err := fs.ReadDir(path)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(fileInfos))
	for _, fi := range fileInfos {
		if fi.IsDir() {
			continue
		}

		matches := migrationPattern.FindStringSubmatch(fi.Name())
		if len(matches) != 2 {
			continue
		}

		n, err := strconv.ParseInt(matches[1], 10, 32)
		if err != nil {
			// The regexp already validated that the prefix is all digits so this *should* never fail
			return nil, err
		}

		mcount := len(paths)

		if n < int64(mcount) {
			return nil, fmt.Errorf("Duplicate migration %d", n)
		}

		if int64(mcount) < n {
			return nil, fmt.Errorf("Missing migration %d", mcount)
		}

		paths = append(paths, filepath.Join(path, fi.Name()))
	}

	return paths, nil
}

func FindMigrations(path string) ([]string, error) {
	return FindMigrationsEx(path, defaultMigratorFS{})
}

func (m *Migrator) LoadMigrations(path string) error {
	path = strings.TrimRight(path, string(filepath.Separator))

	mainTmpl := template.New("main")
	sharedPaths, err := m.options.MigratorFS.Glob(filepath.Join(path, "*", "*.sql"))
	if err != nil {
		return err
	}

	for _, p := range sharedPaths {
		body, err := m.options.MigratorFS.ReadFile(p)
		if err != nil {
			return err
		}

		name := strings.Replace(p, path+string(filepath.Separator), "", 1)
		_, err = mainTmpl.New(name).Parse(string(body))
		if err != nil {
			return err
		}
	}

	paths, err := FindMigrationsEx(path, m.options.MigratorFS)
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		return NoMigrationsFoundError{Path: path}
	}

	for _, p := range paths {
		body, err := m.options.MigratorFS.ReadFile(p)
		if err != nil {
			return err
		}

		pieces := strings.SplitN(string(body), "---- create above / drop below ----", 2)
		var upSQL, downSQL string
		upSQL = strings.TrimSpace(pieces[0])
		upSQL, err = m.evalMigration(mainTmpl.New(filepath.Base(p)+" up"), upSQL)
		if err != nil {
			return err
		}
		// Make sure there is SQL in the forward migration step.
		containsSQL := false
		for _, v := range strings.Split(upSQL, "\n") {
			// Only account for regular single line comment, empty line and space/comment combination
			cleanString := strings.TrimSpace(v)
			if len(cleanString) != 0 &&
				!strings.HasPrefix(cleanString, "--") {
				containsSQL = true
				break
			}
		}
		if !containsSQL {
			return ErrNoFwMigration
		}

		if len(pieces) == 2 {
			downSQL = strings.TrimSpace(pieces[1])
			downSQL, err = m.evalMigration(mainTmpl.New(filepath.Base(p)+" down"), downSQL)
			if err != nil {
				return err
			}
		}

		m.AppendMigration(filepath.Base(p), upSQL, downSQL)
	}

	return nil
}

func (m *Migrator) evalMigration(tmpl *template.Template, sql string) (string, error) {
	tmpl, err := tmpl.Parse(sql)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, m.Data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (m *Migrator) AppendMigration(name, upSQL, downSQL string) {
	m.Migrations = append(
		m.Migrations,
		&Migration{
			Sequence: int32(len(m.Migrations)) + 1,
			Name:     name,
			UpSQL:    upSQL,
			DownSQL:  downSQL,
		})
	return
}

// Migrate runs pending migrations
// It calls m.OnStart when it begins a migration
func (m *Migrator) Migrate() error {
	return m.MigrateTo(int32(len(m.Migrations)))
}

// MigrateTo migrates to targetVersion
func (m *Migrator) MigrateTo(targetVersion int32) (err error) {
	ctx := context.Background()
	// Lock to ensure multiple migrations cannot occur simultaneously
	lockNum := int64(9628173550095224) // arbitrary random number
	if _, lockErr := m.conn.Exec(ctx, "select pg_advisory_lock($1)", lockNum); lockErr != nil {
		return lockErr
	}
	defer func() {
		_, unlockErr := m.conn.Exec(ctx, "select pg_advisory_unlock($1)", lockNum)
		if err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	currentVersion, err := m.GetCurrentVersion()
	if err != nil {
		return err
	}

	if targetVersion < 0 || int32(len(m.Migrations)) < targetVersion {
		errMsg := fmt.Sprintf("destination version %d is outside the valid versions of 0 to %d", targetVersion, len(m.Migrations))
		return BadVersionError(errMsg)
	}

	if currentVersion < 0 || int32(len(m.Migrations)) < currentVersion {
		errMsg := fmt.Sprintf("current version %d is outside the valid versions of 0 to %d", currentVersion, len(m.Migrations))
		return BadVersionError(errMsg)
	}

	var direction int32
	if currentVersion < targetVersion {
		direction = 1
	} else {
		direction = -1
	}

	for currentVersion != targetVersion {
		var current *Migration
		var sql, directionName string
		var sequence int32
		if direction == 1 {
			current = m.Migrations[currentVersion]
			sequence = current.Sequence
			sql = current.UpSQL
			directionName = "up"
		} else {
			current = m.Migrations[currentVersion-1]
			sequence = current.Sequence - 1
			sql = current.DownSQL
			directionName = "down"
			if current.DownSQL == "" {
				return IrreversibleMigrationError{m: current}
			}
		}

		ctx := context.Background()

		tx, err := m.conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)

		// Fire on start callback
		if m.OnStart != nil {
			m.OnStart(current.Sequence, current.Name, directionName, sql)
		}

		// Execute the migration
		_, err = tx.Exec(ctx, sql)
		if err != nil {
			// if err, ok := err.(pgx.PgError); ok {
			// 	return MigrationPgError{Sql: sql, PgError: err}
			// }
			return err
		}

		// Reset all database connection settings. Important to do before updating version as search_path may have been changed.
		tx.Exec(ctx, "reset all")

		// Add one to the version
		_, err = tx.Exec(ctx, "update "+m.versionTable+" set version=$1", sequence)
		if err != nil {
			return err
		}

		err = tx.Commit(ctx)
		if err != nil {
			return err
		}

		currentVersion = currentVersion + direction
	}

	return nil
}

func (m *Migrator) GetCurrentVersion() (v int32, err error) {
	ctx := context.Background()

	err = m.conn.QueryRow(ctx, "select version from "+m.versionTable).Scan(&v)
	return v, err
}

func (m *Migrator) ensureSchemaVersionTableExists() (err error) {
	ctx := context.Background()

	_, err = m.conn.Exec(ctx, fmt.Sprintf(`
    create table if not exists %s(version int4 not null);

    insert into %s(version)
    select 0
    where 0=(select count(*) from %s);
	`, m.versionTable, m.versionTable, m.versionTable))

	return err
}
