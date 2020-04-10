package config

import (
	"testing"
)

func TestInitConf(t *testing.T) {
	_, err := NewConfig("../examples/rails-app/config/supergraph")

	if err != nil {
		t.Fatal(err.Error())
	}
}
