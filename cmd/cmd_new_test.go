package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

func TestCmdNewWritesAgenticAndSourcesTemplates(t *testing.T) {
	log = zap.NewNop().Sugar()
	dbURL = ""
	t.Chdir(t.TempDir())

	cmdNew(nil, []string{"myapp"})

	for _, name := range []string{"dev.yml", "prod.yml", "agentic.yml"} {
		p := filepath.Join("myapp", name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		content := string(b)
		if !strings.Contains(content, "\nsources:\n") {
			t.Fatalf("%s does not use sources config:\n%s", name, content)
		}
		if strings.Contains(content, "\ndatabases:\n") || strings.Contains(content, "\ndatabase:\n") {
			t.Fatalf("%s contains active legacy database config:\n%s", name, content)
		}
	}

	agentic, err := os.ReadFile(filepath.Join("myapp", "agentic.yml"))
	if err != nil {
		t.Fatalf("read agentic.yml: %v", err)
	}
	if !strings.Contains(string(agentic), "mode: agentic") {
		t.Fatalf("agentic.yml missing mode: agentic:\n%s", string(agentic))
	}
}

func TestTemplatesDecodeAsConfig(t *testing.T) {
	tmpl := newTempl(map[string]any{
		"AppName":     "MyApp",
		"AppNameSlug": "myapp",
		"DBType":      "postgres",
		"DBHost":      "db",
		"DBPort":      "5432",
		"DBUser":      "postgres",
		"DBPass":      "postgres",
		"DBName":      "myapp",
	})
	replacer := strings.NewReplacer(
		"${GJ_DATABASE_HOST}", "localhost",
		"${GJ_DATABASE_PORT}", "5432",
		"${GJ_DATABASE_NAME}", "myapp",
		"${GJ_DATABASE_USER}", "postgres",
		"${GJ_DATABASE_PASSWORD}", "postgres",
		"${GJ_DATABASE_URL}", "postgres://postgres:postgres@localhost:5432/myapp?sslmode=disable",
	)

	for _, name := range []string{"dev.yml", "prod.yml", "agentic.yml"} {
		b, err := tmpl.get(name)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if _, err := serv.NewConfig(replacer.Replace(string(b)), "yaml"); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
	}
}

func TestRenderedTemplatesReadWithInheritance(t *testing.T) {
	tmpl := newTempl(map[string]any{
		"AppName":     "MyApp",
		"AppNameSlug": "myapp",
		"DBType":      "postgres",
		"DBHost":      "db",
		"DBPort":      "5432",
		"DBUser":      "postgres",
		"DBPass":      "postgres",
		"DBName":      "myapp",
	})
	replacer := strings.NewReplacer(
		"${GJ_DATABASE_HOST}", "localhost",
		"${GJ_DATABASE_PORT}", "5432",
		"${GJ_DATABASE_NAME}", "myapp",
		"${GJ_DATABASE_USER}", "postgres",
		"${GJ_DATABASE_PASSWORD}", "postgres",
	)
	fs := afero.NewMemMapFs()
	for _, name := range []string{"dev.yml", "prod.yml", "agentic.yml"} {
		b, err := tmpl.get(name)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if err := afero.WriteFile(fs, "/"+name, []byte(replacer.Replace(string(b))), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for _, name := range []string{"dev.yml", "prod.yml", "agentic.yml"} {
		conf, err := serv.ReadInConfigFS("/"+name, fs)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		seen := map[string]bool{}
		for _, source := range conf.Core.Sources {
			if seen[source.Name] {
				t.Fatalf("%s has duplicate source %q after inheritance: %+v", name, source.Name, conf.Core.Sources)
			}
			seen[source.Name] = true
		}
	}
}

func TestSetupCreatesTemplateSet(t *testing.T) {
	log = zap.NewNop().Sugar()
	oldConf := conf
	oldCpath := cpath
	defer func() {
		conf = oldConf
		cpath = oldCpath
	}()
	conf = nil
	t.Setenv("GO_ENV", "dev")

	dir := filepath.Join(t.TempDir(), "config")
	setup(dir)

	for _, name := range []string{"dev.yml", "prod.yml", "agentic.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected setup to create %s: %v", name, err)
		}
	}
}

func TestTemplatesRenderSourcesMode(t *testing.T) {
	tmpl := newTempl(map[string]any{
		"AppName":     "MyApp",
		"AppNameSlug": "myapp",
		"DBType":      "postgres",
		"DBHost":      "db",
		"DBPort":      "5432",
		"DBUser":      "postgres",
		"DBPass":      "postgres",
		"DBName":      "myapp",
	})

	for _, name := range []string{"dev.yml", "prod.yml", "agentic.yml"} {
		b, err := tmpl.get(name)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		content := string(b)
		if !strings.Contains(content, "\nsources:\n") {
			t.Fatalf("%s missing sources:\n%s", name, content)
		}
		if strings.Contains(content, "\ndatabases:\n") || strings.Contains(content, "\ndatabase:\n") {
			t.Fatalf("%s contains active legacy database config:\n%s", name, content)
		}
	}
}
