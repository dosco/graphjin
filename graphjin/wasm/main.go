//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/dosco/graphjin/serv"
)

func main() {
	var fn js.Func

	fn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		var confPath string

		if len(args) == 1 {
			confPath = args[0].String()
		}

		h := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resolve := args[0]
			reject := args[1]

			if confPath == "" {
				jsErr := js.Global().Get("Error").New("config argument missing")
				reject.Invoke(jsErr)
				return nil
			}

			go func() {
				err := initGraphJinServ(confPath)
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

	js.Global().Set("startGraphJin", fn)
	c := make(chan bool)
	<-c
}

func initGraphJinServ(config string) error {
	conf, err := serv.NewConfig(config, "yaml")
	if err != nil {
		return err
	}
	conf.WatchAndReload = false
	conf.HotDeploy = false
	conf.Core.DisableAllowList = true

	gj, err := serv.NewGraphJinService(conf)
	if err != nil {
		return err
	}

	if err := gj.Start(); err != nil {
		return err
	}

	return nil
}
