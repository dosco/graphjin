package serv

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"strings"
)

const gjM1 = `
CREATE SCHEMA IF NOT EXISTS _graphjin;

CREATE TABLE _graphjin.params (
	key text NOT NULL UNIQUE,
	value text NOT NULL DEFAULT ''
);

INSERT INTO 
	_graphjin.params (key, value) 
VALUES 
	('admin.version', '1');

CREATE TABLE _graphjin.configs (
	id bigint {{ .idCol }},
	previous_id bigint NOT NULL DEFAULT -1,
	name text NOT NULL UNIQUE,
	hash text NOT NULL UNIQUE,
	active bool NOT NULL DEFAULT FALSE,
	bundle text NOT NULL
);

CREATE INDEX config_active ON _graphjin.configs (active);
`

func InitAdmin(db *sql.DB, dbtype string) error {
	c := context.Background()

	tmpl := template.Must(template.New("sql").Parse(gjM1))
	stmt := strings.Builder{}

	err := tmpl.Execute(&stmt, map[string]interface{}{
		"idCol": idColSql(dbtype),
	})
	if err != nil {
		panic(err)
	}

	if _, err := db.ExecContext(c, stmt.String()); err != nil {
		return fmt.Errorf("error migrating admin schema: %w", err)
	}
	return nil
}

func idColSql(dbtype string) string {
	switch dbtype {
	case "mysql":
		return "NOT NULL AUTO_INCREMENT PRIMARY KEY"
	default:
		return "GENERATED ALWAYS AS IDENTITY PRIMARY KEY"
	}
}
