package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

//go:embed benchmark/public-suite.json
var publicEvalSuiteFS embed.FS

func loadPublicEvalSuite() (*gjeval.Suite, error) {
	data, err := publicEvalSuiteFS.ReadFile("benchmark/public-suite.json")
	if err != nil {
		return nil, err
	}
	var suite gjeval.Suite
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		return nil, fmt.Errorf("parse embedded public benchmark suite: %w", err)
	}
	if err := suite.Validate(); err != nil {
		return nil, fmt.Errorf("validate embedded public benchmark suite: %w", err)
	}
	return &suite, nil
}
