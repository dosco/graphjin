package serv

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/afero/zipfs"
)

const adminVersion = 1

type depResp struct {
	name, pname string
}

func (s *service) saveConfig(c context.Context, name, bundle string) (*depResp, error) {
	var dres depResp

	zip, err := base64.StdEncoding.DecodeString(bundle)
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	_, _ = h.Write(zip)
	hash := base64.URLEncoding.EncodeToString(h.Sum(nil))

	opt := &sql.TxOptions{Isolation: sql.LevelSerializable}
	tx, err := s.db.BeginTx(c, opt)
	if err != nil {
		return nil, err
	}

	if _, err := getAdminParams(tx); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("error in admin schema: %w", err)
	}

	previousID := -1

	// find previous active id
	err = tx.QueryRow(`
	SELECT
		id,
		name
	FROM 
		_graphjin.configs
	WHERE
		(active = TRUE)`).Scan(&previousID, &dres.pname)

	if err != nil && err != sql.ErrNoRows {
		_ = tx.Rollback()
		return nil, err
	}

	id := -1
	phash := ""

	// check if current config already exists in db
	err = tx.QueryRow(`
	SELECT
		id,
		name,
		hash
	FROM 
		_graphjin.configs
	WHERE
		(hash = $1 OR name = $2)
`, hash, name).Scan(&id, &dres.name, &phash)

	if err != nil && err != sql.ErrNoRows {
		_ = tx.Rollback()
		return nil, err
	}

	if hash == phash {
		_ = tx.Rollback()
		return &dres, nil
	}

	if previousID != -1 {
		_, err = tx.Exec(`
		UPDATE 
			_graphjin.configs 
		SET 
			active = FALSE 
		WHERE 
		id = $1`, previousID)

		if err != nil && err != sql.ErrNoRows {
			_ = tx.Rollback()
			return nil, err
		}
	}

	// if current config does not exist then insert
	if id == -1 {
		dres.name = name
		_, err = tx.Exec(`
		INSERT INTO
			_graphjin.configs (previous_id, name, hash, active, bundle)
		VALUES
			($1, $2, $3, TRUE, $4)`, previousID, name, hash, bundle)

		// if current config exists then update
	} else {
		_, err = tx.Exec(`
		UPDATE
			_graphjin.configs
		SET
			previous_id = $1,
			active = TRUE,
			hash = $2,
			bundle = $3
		WHERE
			id = $4`, previousID, hash, bundle, id)
	}

	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	return &dres, nil
}

func (s *service) rollbackConfig(c context.Context) (*depResp, error) {
	var dres depResp

	opt := &sql.TxOptions{Isolation: sql.LevelSerializable}
	tx, err := s.db.BeginTx(c, opt)
	if err != nil {
		return nil, err
	}

	if _, err := getAdminParams(tx); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("error in admin schema: %w", err)
	}

	id := -1
	previousID := -1

	// find previous active id
	err = tx.QueryRow(`
	SELECT
		cc.id,
		cc.previous_id,
		cc.name,
		coalesce(pc.name, '')
	FROM 
		_graphjin.configs cc
	LEFT JOIN 
		_graphjin.configs pc ON pc.id = cc.previous_id
	WHERE
		(cc.active = TRUE)`).Scan(&id, &previousID, &dres.name, &dres.pname)

	if err != nil && err != sql.ErrNoRows {
		_ = tx.Rollback()
		return nil, err
	}

	if previousID != -1 {
		_, err = tx.Exec(`
	UPDATE 
		_graphjin.configs 
	SET 
		active = (CASE id WHEN $1 THEN FALSE WHEN $2 THEN TRUE END)
	WHERE 
		(id = $1 OR id = $2)`, id, previousID)

		if err != nil && err != sql.ErrNoRows {
			_ = tx.Rollback()
			return nil, err
		}
	}

	// check if current config already exists in db
	_, err = tx.Exec(`
	DELETE FROM 
		_graphjin.configs
	WHERE
		(id = $1)
`, id)

	if err != nil && err != sql.ErrNoRows {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	return &dres, nil
}

type adminParams struct {
	version int
	params  map[string]string
}

func getAdminParams(tx *sql.Tx) (adminParams, error) {
	var ap adminParams

	rows, err := tx.Query(`
	SELECT
		key,
		value
	FROM
		_graphjin.params`)
	if err != nil {
		return ap, err
	}
	defer rows.Close()

	ap.params = make(map[string]string)

	for rows.Next() {
		var k, v string

		if err := rows.Scan(&k, &v); err != nil {
			return ap, err
		}
		ap.params[k] = v
	}

	if v, ok := ap.params["admin.version"]; ok {
		if ap.version, err = strconv.Atoi(v); err != nil {
			return ap, err
		}
	} else {
		return ap, fmt.Errorf("missing param: admin.version")
	}

	switch {
	case ap.version < adminVersion:
		return ap, fmt.Errorf("please upgrade graphjin admin to latest version")
	case ap.version > adminVersion:
		return ap, fmt.Errorf("please upgrade graphjin cli to the latest")
	}

	return ap, nil
}

func startHotDeployWatcher(s1 *Service) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s := s1.Load().(*service)

		cf := s.conf.vi.ConfigFileUsed()
		cf = filepath.Join("/", filepath.Base(strings.TrimSuffix(cf, filepath.Ext(cf))))

		var id int
		var hash string
		err := s.db.QueryRow(`
			SELECT
				id,
				hash
			FROM
				_graphjin.configs
			WHERE 
				active = TRUE`).Scan(&id, &hash)

		if err != nil && err != sql.ErrNoRows {
			return err
		}

		if err == sql.ErrNoRows {
			continue
		}

		if hash == s.chash {
			continue
		}

		var name, bundle string

		err = s.db.QueryRow(`
			SELECT
				name,
				bundle
			FROM
				_graphjin.configs
			WHERE 
				id = $1`, id).Scan(&name, &bundle)

		if err != nil {
			return err
		}

		if err := deployBundle(s1, name, hash, cf, bundle); err != nil {
			s.log.Errorf("failed to deploy: %s", err.Error())
			continue
		}

		s.log.Infof("deployment successful: %s", name)
	}

	return nil
}

type activeBundle struct {
	name, hash, bundle string
}

func fetchActiveBundle(db *sql.DB) (*activeBundle, error) {
	var ab activeBundle

	err := db.QueryRow(`
	SELECT
		name,
		hash,
		bundle
	FROM
		_graphjin.configs
	WHERE 
		active = TRUE`).Scan(&ab.name, &ab.hash, &ab.bundle)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &ab, nil
}

func deployBundle(s1 *Service, name, hash, confFile, bundle string) error {
	bfs, err := bundle2Fs(name, hash, confFile, bundle)
	if err != nil {
		return err
	}
	fsOption := OptionSetFS(newAferoFS(bfs.fs, "/"))
	return s1.Deploy(bfs.conf, fsOption)
}

type bundleFs struct {
	conf *Config
	fs   afero.Fs
}

func bundle2Fs(name, hash, confFile, bundle string) (bundleFs, error) {
	var bfs bundleFs

	b, err := base64.StdEncoding.DecodeString(bundle)
	if err != nil {
		return bfs, err
	}

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return bfs, err
	}

	bfs.fs = zipfs.New(zr)
	bfs.conf, err = ReadInConfigFS(confFile, bfs.fs)
	if err != nil {
		return bfs, err
	}
	bfs.conf.SetHash(hash)
	bfs.conf.SetName(name)
	return bfs, nil
}
