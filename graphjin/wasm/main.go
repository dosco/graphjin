//go:build wasm && js

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"syscall/js"

	"github.com/dosco/graphjin/conf/v3"
	"github.com/dosco/graphjin/core/v3"
)

func main() {
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

		fs := NewJSFS(fsv, cpv.String())

		confVal := cov.Get("value").String()
		confValIsFile := cov.Get("isFile").Bool()

		conf, err := getConfig(confVal, confValIsFile, fs)
		if err != nil {
			return toJSError(err)
		}

		var db *sql.DB
		switch conf.DBType {
		case "mysql":
			db = sql.OpenDB(NewMyDBConn(dbv))
		default:
			db = sql.OpenDB(NewPgDBConn(dbv))
		}

		h := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resolve := args[0]
			reject := args[1]

			go func() {
				gj, err := newGraphJin(conf, db, fs)
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

func newGraphJin(config *core.Config, db *sql.DB, fs core.FS) (gj *core.GraphJin, err error) {
	return core.NewGraphJinWithFS(config, db, fs)
}

func getConfig(config string, confIsFile bool, fs core.FS) (
	c *core.Config, err error,
) {
	if confIsFile {
		if c, err = conf.NewConfigWithFS(fs, config); err != nil {
			return
		}
	} else {
		c = &core.Config{}
		if err = json.Unmarshal([]byte(config), c); err != nil {
			return
		}
	}
	return
}
