//go:build cgo

package codesql

import (
	"context"
	"database/sql"
	"strings"
)

func (idx *indexer) upsertLanguageMetadata(ctx context.Context) error {
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, spec := range idx.languages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO code_languages(name, extensions, parser, enabled)
		  VALUES (?, ?, ?, 1)
		  ON CONFLICT(name) DO UPDATE SET extensions = excluded.extensions, parser = excluded.parser, enabled = 1`,
			spec.Name, strings.Join(spec.Extensions, ","), spec.ParserName); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		var languageID int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM code_languages WHERE name = ?`, spec.Name).Scan(&languageID); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO code_grammars(language_id, name, version, abi_version)
		  VALUES (?, ?, ?, ?)
		  ON CONFLICT(language_id, name) DO UPDATE SET version = excluded.version, abi_version = excluded.abi_version`,
			languageID, spec.ParserName, grammarHash(spec), spec.Language.AbiVersion()); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		for _, qp := range spec.QueryPacks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO code_query_packs(language_id, kind, hash, source)
			  VALUES (?, ?, ?, ?)
			  ON CONFLICT(language_id, kind) DO UPDATE SET hash = excluded.hash, source = excluded.source`,
				languageID, qp.Kind, qp.Hash, qp.Source); err != nil {
				tx.Rollback() //nolint:errcheck
				return err
			}
		}
	}
	return tx.Commit()
}

func hasFile(ctx context.Context, tx *sql.Tx, path string) bool {
	var n int
	_ = tx.QueryRowContext(ctx, `SELECT 1 FROM code_files WHERE path = ?`, path).Scan(&n)
	return n == 1
}
