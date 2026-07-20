package main

import (
	"bytes"
	"embed"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var dbURL string

// This is the cobra CLI command for the new subcommand
func newCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "new <app-name>",
		Short: "Create a new application",
		Long: `Generate the files required to start a new GraphJin app.

The generated dev and agentic configs rely on GraphJin's mode defaults:
managed artifacts, watches, the built-in agent, stateful MCP HTTP, and the
primitive MCP tools need no feature toggles. Production defaults are unchanged.`,
		Run: cmdNew,
	}

	c.PersistentFlags().StringVar(&dbURL, "db-url", "", "URL of the database")
	return c
}

// cmdNew is the handler for the new subcommand
func cmdNew(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help() //nolint:errcheck
		os.Exit(1)
	}

	dbType := "postgres"
	dbHost := "db"
	dbPort := "5432"
	dbName := ""
	dbUser := "postgres"
	dbPass := "postgres"

	if dbURL != "" {
		u, err := url.Parse(dbURL)
		if err != nil {
			log.Fatal(err)
		}
		dbType = u.Scheme
		dbHost = u.Hostname()

		if v := u.Port(); v != "" {
			dbPort = v
		} else if dbType == "mysql" {
			dbPort = "3306"
		}

		if v := u.User.Username(); v != "" {
			dbUser = v
		} else if dbType == "mysql" {
			dbUser = "root"
		}

		if v, ok := u.User.Password(); ok {
			dbPass = v
		} else if dbType == "mysql" {
			dbPass = ""
		}

		if v := u.Path; len(v) > 1 {
			dbName = v[1:]
		}
	}

	en := cases.Title(language.English)
	tmpl := newTempl(map[string]interface{}{
		"AppName":     en.String(strings.Join(args, " ")),
		"AppNameSlug": strings.ToLower(strings.Join(args, "_")),
		"DBType":      dbType,
		"DBHost":      dbHost,
		"DBPort":      dbPort,
		"DBUser":      dbUser,
		"DBPass":      dbPass,
		"DBName":      dbName,
	})

	// Create app folder and add relevant files

	name := args[0]
	appPath := filepath.Join("./", name)

	ifNotExists(appPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appPath, "dev.yml"), func(p string) error {
		if v, err := tmpl.get("dev.yml"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appPath, "prod.yml"), func(p string) error {
		if v, err := tmpl.get("prod.yml"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appPath, "agentic.yml"), func(p string) error {
		if v, err := tmpl.get("agentic.yml"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	// The config JSON Schema referenced by the yaml-language-server modeline
	// at the top of each config file; gives editors autocomplete and docs.
	ifNotExists(path.Join(appPath, configSchemaFile), func(p string) error {
		return os.WriteFile(p, serv.ConfigJSONSchema(), 0o600)
	})

	log.Infof("App initialized: %s", name)
}

// configSchemaFile is the schema filename the config templates reference in
// their yaml-language-server modeline.
const configSchemaFile = "config.schema.json"

//go:embed tmpl
var tmpl embed.FS

type Templ struct {
	values map[string]interface{}
}

func newTempl(values map[string]interface{}) *Templ {
	return &Templ{values}
}

func (t *Templ) get(name string) ([]byte, error) {
	v, err := tmpl.ReadFile("tmpl/" + name)
	if err != nil {
		return nil, err
	}

	b := bytes.Buffer{}

	tmpl, err := template.New(name).Parse(string(v))
	if err != nil {
		return nil, err
	}

	if err := tmpl.Execute(&b, t.values); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func ifNotExists(filePath string, doFn func(string) error) {
	_, err := os.Stat(filePath)

	if err == nil {
		log.Infof("Create skipped file exists: %s", filePath)
		return
	}

	if !os.IsNotExist(err) {
		log.Fatalf("Error checking if file exists: %s", filePath)
	}

	err = doFn(filePath)
	if err != nil {
		log.Fatalf("%s: %s", err, filePath)
	}

	log.Infof("Created: %s", filePath)
}
