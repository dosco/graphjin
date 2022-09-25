//go:build js && wasm

package main

import (
	"database/sql"
	"fmt"
	"syscall/js"

	"github.com/dosco/graphjin/serv"
	"github.com/spf13/afero"
)

func main() {
	var fn js.Func

	fn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) != 2 {
			return nil
		}

		confPath := args[0].String()
		db := args[1]

		h := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resolve := args[0]
			reject := args[1]

			if confPath == "" {
				jsErr := js.Global().Get("Error").New("config argument missing")
				reject.Invoke(jsErr)
				return nil
			}

			go func() {
				err := initGraphJinServ(confPath, db)
				if err != nil {
					jsErr := js.Global().Get("Error").New(err.Error())
					reject.Invoke(jsErr)
				} else {
					resolve.Invoke( /*js.ValueOf(nil)*/ )
				}
			}()

			return nil
		})

		return js.Global().Get("Promise").New(h)
	})

	sql.Register("postgres", &Driver{})
	js.Global().Set("startGraphJin", fn)
	c := make(chan bool)
	<-c
}

func initGraphJinServ(config string, client js.Value, fs js.Value) error {
	conf, err := serv.NewConfig(config, "yaml")
	if err != nil {
		return err
	}
	conf.WatchAndReload = false
	conf.HotDeploy = false
	conf.Core.DisableAllowList = true

	db := sql.OpenDB(NewDriverConn(client))

	gj, err := serv.NewGraphJinService(conf,
		// serv.OptionSetFS(NewFs(fs)),
		serv.OptionSetDB(db))
	// gj, err := serv.NewGraphJinService(conf)

	if err != nil {
		return err
	}

	// var sourceID int
	// row := db.QueryRow("select id from papers where limit 3", 1, 2)
	// err = row.Scan(&sourceID)
	// fmt.Println(">>>>", sourceID, err)
	// return nil

	if err := gj.Start(); err != nil {
		return err
	}

	fmt.Println(">>>>", gj, err)

	return nil
}
