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

	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var dbURL string

func newCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "new <app-name>",
		Short: "Create a new application",
		Long:  "Generate all the required files to start on a new GraphJin app",
		Run:   cmdNew,
	}

	c.PersistentFlags().StringVar(&dbURL, "db-url", "", "URL of the database")
	return c
}

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
		dbHost = u.Host

		if v := u.Port(); v != "" {
			dbPort = v
		} else if dbType == "mysql" {
			dbPort = "3306"
		}

		if v := u.User.Username(); v != "" {
			dbUser = v
		} else if dbType == "mysql" {
			dbPort = "root"
		}

		if v, ok := u.User.Password(); ok {
			dbPass = v
		} else if dbType == "mysql" {
			dbPort = ""
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

	ifNotExists(path.Join(appPath, "Dockerfile"), func(p string) error {
		if v, err := tmpl.get("Dockerfile"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	if dbURL == "" {
		ifNotExists(path.Join(appPath, "docker-compose.yml"), func(p string) error {
			if v, err := tmpl.get("docker-compose.yml"); err == nil {
				return os.WriteFile(p, v, 0o600)
			} else {
				return err
			}
		})
	}

	// Create app config folder and add relevant files

	appConfigPath := filepath.Join(appPath, "config")

	ifNotExists(appConfigPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appConfigPath, "dev.yml"), func(p string) error {
		if v, err := tmpl.get("dev.yml"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appConfigPath, "prod.yml"), func(p string) error {
		if v, err := tmpl.get("prod.yml"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appConfigPath, "seed.js"), func(p string) error {
		if v, err := tmpl.get("seed.js"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	// Create app migrations folder and add relevant files

	appMigrationsPath := filepath.Join(appConfigPath, "migrations")

	ifNotExists(appMigrationsPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appMigrationsPath, "0_init.sql"), func(p string) error {
		if v, err := tmpl.get("0_init.sql"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	// Create folder to hold scripts

	scriptsPath := filepath.Join(appConfigPath, "scripts")

	ifNotExists(scriptsPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	// Create queries folder and add a sample query

	appQueriesPath := filepath.Join(appConfigPath, "queries")

	ifNotExists(appQueriesPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appQueriesPath, "getUsers.gql"), func(p string) error {
		if v, err := tmpl.get("getUsers.gql"); err == nil {
			return os.WriteFile(p, v, 0o600)
		} else {
			return err
		}
	})

	log.Infof("App initialized: %s", name)
}

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
