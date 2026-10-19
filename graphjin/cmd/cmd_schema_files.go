package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

type schemaDDLFile struct {
	Source string
	Path   string
	Data   []byte
}

func projectSchemaDDLPath() string {
	return filepath.Join(cpath, core.SchemaDDLFile)
}

func legacySchemaGraphQLPath() string {
	return filepath.Join(cpath, core.LegacySchemaGraphQLFile)
}

func sourceSchemaDDLDir() string {
	return filepath.Join(cpath, core.SourceSchemaDDLDir)
}

func readProjectSchemaDDL() ([]byte, string, error) {
	for _, path := range []string{projectSchemaDDLPath(), legacySchemaGraphQLPath()} {
		data, err := os.ReadFile(path)
		if err == nil {
			if filepath.Base(path) == core.LegacySchemaGraphQLFile {
				log.Warnf("%s is deprecated; rename it to %s", core.LegacySchemaGraphQLFile, core.SchemaDDLFile)
			}
			return data, path, nil
		}
		if !os.IsNotExist(err) {
			return nil, path, err
		}
	}
	return nil, projectSchemaDDLPath(), fmt.Errorf("%s not found (legacy %s also checked)",
		core.SchemaDDLFile, core.LegacySchemaGraphQLFile)
}

func sourceSchemaDDLFiles() ([]schemaDDLFile, error) {
	entries, err := os.ReadDir(sourceSchemaDDLDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var files []schemaDDLFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ddl") {
			continue
		}
		source := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		path := filepath.Join(sourceSchemaDDLDir(), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, schemaDDLFile{Source: source, Path: path, Data: data})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Source < files[j].Source })
	return files, nil
}

func hasProjectSchemaDDL() (bool, error) {
	_, err := os.Stat(projectSchemaDDLPath())
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func ensureNoSchemaDDLAmbiguity(files []schemaDDLFile) error {
	if len(files) == 0 {
		return nil
	}
	hasProject, err := hasProjectSchemaDDL()
	if err != nil {
		return err
	}
	if hasProject {
		return fmt.Errorf("ambiguous schema DDL: found both %s and %s/*.ddl; keep one layout",
			core.SchemaDDLFile, core.SourceSchemaDDLDir)
	}
	return nil
}
