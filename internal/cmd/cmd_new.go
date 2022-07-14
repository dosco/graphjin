package cmd

import (
	"bytes"
	"embed"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func newCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "new <app-name>",
		Short: "Create a new application",
		Long:  "Generate all the required files to start on a new GraphJin app",
		Run:   cmdNew,
	}
	return c
}

func cmdNew(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help() //nolint: errcheck
		os.Exit(1)
	}

	en := cases.Title(language.English)
	tmpl := newTempl(map[string]string{
		"AppName":     en.String(strings.Join(args, " ")),
		"AppNameSlug": strings.ToLower(strings.Join(args, "_")),
	})

	// Create app folder and add relevant files

	name := args[0]
	appPath := path.Join("./", name)

	ifNotExists(appPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appPath, "Dockerfile"), func(p string) error {
		if v, err := tmpl.get("Dockerfile"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appPath, "docker-compose.yml"), func(p string) error {
		if v, err := tmpl.get("docker-compose.yml"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	// Create app config folder and add relevant files

	appConfigPath := path.Join(appPath, "config")

	ifNotExists(appConfigPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appConfigPath, "dev.yml"), func(p string) error {
		if v, err := tmpl.get("dev.yml"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appConfigPath, "prod.yml"), func(p string) error {
		if v, err := tmpl.get("prod.yml"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	ifNotExists(path.Join(appConfigPath, "seed.js"), func(p string) error {
		if v, err := tmpl.get("seed.js"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	// Create app migrations folder and add relevant files

	appMigrationsPath := path.Join(appConfigPath, "migrations")

	ifNotExists(appMigrationsPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appMigrationsPath, "0_init.sql"), func(p string) error {
		if v, err := tmpl.get("0_init.sql"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	// Create folder to hold scripts

	scriptsPath := path.Join(appConfigPath, "scripts")

	ifNotExists(scriptsPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	// Create queries folder and add a sample query

	appQueriesPath := path.Join(appConfigPath, "queries")

	ifNotExists(appQueriesPath, func(p string) error {
		return os.Mkdir(p, os.ModePerm)
	})

	ifNotExists(path.Join(appQueriesPath, "getUsers.gql"), func(p string) error {
		if v, err := tmpl.get("getUsers.gql"); err == nil {
			return ioutil.WriteFile(p, v, 0600)
		} else {
			return err
		}
	})

	log.Infof("App initialized: %s", name)
}

//go:embed tmpl
var tmpl embed.FS

type Templ struct {
	values map[string]string
}

func newTempl(values map[string]string) *Templ {
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
