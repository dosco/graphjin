//go:build js && wasm

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"syscall/js"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/plugin"
)

func main() {
	sql.Register("postgres", &JSPostgresDB{})
	js.Global().Set("createGraphJin", graphjinFunc())
	<-make(chan bool)
}

func graphjinFunc() js.Func {
	fn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		var err error

		if len(args) != 4 {
			err := errors.New("required arguments: config path, config, database and filesystem")
			return toJSError(err)
		}

		cpv := args[0]
		cov := args[1]
		dbv := args[2]
		fsv := args[3]

		if cpv.Type() != js.TypeString || cpv.String() == "" {
			err = errors.New("config path argument missing")
		}

		if cov.Type() != js.TypeObject || cov.Get("value").String() == "" {
			err = errors.New("config file / value argument missing")
		}

		if dbv.Type() != js.TypeObject {
			err = errors.New("database argument missing")
		}

		if fsv.Type() != js.TypeObject {
			err = errors.New("filesystem argument missing")
		}

		if err != nil {
			return toJSError(err)
		}

		conf := cov.Get("value").String()
		confIsFile := cov.Get("isFile").Bool()

		db := sql.OpenDB(NewJSPostgresDBConn(dbv))
		fs := NewJSFSWithBase(fsv, cpv.String())

		h := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resolve := args[0]
			reject := args[1]

			go func() {
				gj, err := newGraphJin(conf, confIsFile, db, fs)
				if err != nil {
					reject.Invoke(toJSError(err))
				} else {
					resolve.Invoke(newGraphJinObj(gj))
				}
			}()
			return nil
		})
		return js.Global().Get("Promise").New(h)
	})
	return fn
}

func newGraphJinObj(gj *core.GraphJin) map[string]interface{} {
	return map[string]interface{}{
		"query":           query(gj),
		"subscribe":       subscribe(gj),
		"queryByName":     queryByName(gj),
		"subscribeByName": subscribeByName(gj),
	}
}

func newGraphJin(
	conf string,
	confIsFile bool,
	db *sql.DB,
	fs plugin.FS) (gj *core.GraphJin, err error) {

	confFormat := "json"

	if confIsFile {
		ext := filepath.Ext(conf)
		switch ext {
		case ".json":
			confFormat = "json"
		case ".yml", ".yaml":
			confFormat = "yaml"
		default:
			return nil, fmt.Errorf("invalid config file format: %s", ext)
		}

		if v, err := fs.ReadFile("dev.yml"); err != nil {
			return nil, err
		} else {
			conf = string(v)
		}
	}

	config, err := core.NewConfig(conf, confFormat)
	if err != nil {
		return nil, err
	}

	return core.NewGraphJin(config, db, core.OptionSetFS(fs))
}
