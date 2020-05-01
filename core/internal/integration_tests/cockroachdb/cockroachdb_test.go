package cockraochdb_test

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sync/atomic"
	"testing"

	integration_tests "github.com/dosco/super-graph/core/internal/integration_tests"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/stretchr/testify/require"
)

func TestCockroachDB(t *testing.T) {

	dir, err := ioutil.TempDir("", "temp-cockraochdb-")
	if err != nil {
		log.Fatal(err)
	}

	cmd := exec.Command("cockroach", "start", "--insecure", "--listen-addr", ":0", "--http-addr", ":0", "--store=path="+dir)
	finder := &urlFinder{
		c: make(chan bool),
	}
	cmd.Stdout = finder
	cmd.Stderr = ioutil.Discard

	err = cmd.Start()
	if err != nil {
		t.Skip("is CockroachDB installed?: " + err.Error())
	}
	fmt.Println("started temporary cockroach db")

	stopped := int32(0)
	stopDatabase := func() {
		fmt.Println("stopping temporary cockroach db")
		if atomic.CompareAndSwapInt32(&stopped, 0, 1) {
			if err := cmd.Process.Kill(); err != nil {
				log.Fatal(err)
			}
			if _, err := cmd.Process.Wait(); err != nil {
				log.Fatal(err)
			}
			os.RemoveAll(dir)
		}
	}
	defer stopDatabase()

	// Wait till we figure out the URL we should connect to...
	<-finder.c
	db, err := sql.Open("pgx", finder.URL)
	if err != nil {
		stopDatabase()
		require.NoError(t, err)
	}
	integration_tests.SetupSchema(t, db)

	integration_tests.TestSuperGraph(t, db, func(t *testing.T) {
		if t.Name() == "TestCockroachDB/nested_insert" {
			t.Skip("nested inserts currently not working yet on cockroach db")
		}
	})
}

type urlFinder struct {
	c    chan bool
	done bool
	URL  string
}

func (finder *urlFinder) Write(p []byte) (n int, err error) {
	s := string(p)
	urlRegex := regexp.MustCompile(`\nsql:\s+(postgresql:[^\s]+)\n`)
	if !finder.done {
		submatch := urlRegex.FindAllStringSubmatch(s, -1)
		if submatch != nil {
			finder.URL = submatch[0][1]
			finder.done = true
			close(finder.c)
		}
	}
	return len(p), nil
}
