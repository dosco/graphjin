package serv

import (
	"testing"
)

func TestInitConf(t *testing.T) {
	_, err := initConf()

	if err != nil {
		t.Fatal(err.Error())
	}
}
