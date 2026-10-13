package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

const (
	DeepContosoBenchmarkSlug             = "deepcontoso"
	DeepContosoBenchmarkSuiteFingerprint = "6d23b0e3399aba54ab9e9c9174feab09"
)

//go:embed benchmark/public-suite.json benchmark/deepcontoso-suite.json
var publicEvalSuiteFS embed.FS

func loadPublicEvalSuite() (*gjeval.Suite, error) {
	return loadEmbeddedEvalSuite("benchmark/public-suite.json", "public")
}

func loadDeepContosoEvalSuite() (*gjeval.Suite, error) {
	return loadEmbeddedEvalSuite("benchmark/deepcontoso-suite.json", "deepcontoso")
}

func loadNamedEvalSuite(name string) (*gjeval.Suite, error) {
	switch name {
	case DeepContosoBenchmarkSlug:
		return loadDeepContosoEvalSuite()
	case "", "deeporg":
		return loadPublicEvalSuite()
	default:
		return nil, fmt.Errorf("unknown public benchmark suite: %q", name)
	}
}

func loadEmbeddedEvalSuite(filename, label string) (*gjeval.Suite, error) {
	data, err := publicEvalSuiteFS.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var suite gjeval.Suite
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		return nil, fmt.Errorf("parse embedded %s benchmark suite: %w", label, err)
	}
	if err := suite.Validate(); err != nil {
		return nil, fmt.Errorf("validate embedded %s benchmark suite: %w", label, err)
	}
	return &suite, nil
}
