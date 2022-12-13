//go:build js && wasm

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"syscall/js"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/serv"
)

func query(gj *core.GraphJin) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		qa, err := newQueryArgs(args)
		if !err.IsNull() {
			return err
		}

		c := context.TODO()
		if qa.userID != nil {
			c = context.WithValue(c, core.UserIDKey, qa.userID)
		}

		fn := func(resolve, reject js.Value) {
			res, err := gj.GraphQL(c, qa.query, qa.vars, nil)
			if err != nil {
				reject.Invoke(toJSError(err))
			} else {
				resolve.Invoke(fromResult(res))
			}
		}
		return toAwait(fn)
	})
}

func querybyName(gjs *serv.Service) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		qa, err := newQueryByNameArgs(args)
		if !err.IsNull() {
			return err
		}

		gj := gjs.GetGraphJin()
		c := context.TODO()

		if qa.userID != nil {
			c = context.WithValue(c, core.UserIDKey, qa.userID)
		}

		fn := func(resolve, reject js.Value) {
			res, err := gj.GraphQLByName(c, qa.query, qa.vars, nil)
			if err != nil {
				reject.Invoke(toJSError(err))
			} else {
				resolve.Invoke(fromResult(res))
			}
		}
		return toAwait(fn)
	})
}

func subscribe(gj *core.GraphJin) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		qa, err := newQueryArgs(args)
		if !err.IsNull() {
			return err
		}

		c := context.TODO()
		if qa.userID != nil {
			c = context.WithValue(c, core.UserIDKey, qa.userID)
		}

		fn := func(resolve, reject js.Value) {
			res, err := gj.Subscribe(c, qa.query, qa.vars, nil)
			if err != nil {
				reject.Invoke(toJSError(err))
			} else {
				resolve.Invoke(fromMember(res))
			}
		}
		return toAwait(fn)
	})
}

func fromResult(res *core.Result) map[string]interface{} {
	sql := res.SQL()
	sqlFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return sql
	})

	data := string(res.Data)
	dataFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.Global().Get("JSON").Call("parse", data)
	})

	hash := res.Hash
	hashFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return hex.EncodeToString(hash[:])
	})

	return map[string]interface{}{
		"sql":  sqlFn,
		"data": dataFn,
		"hash": hashFn,
		"role": res.Role(),
	}
}

func fromMember(m *core.Member) map[string]interface{} {
	dataFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) == 0 || args[0].Type() != js.TypeFunction {
			err := errors.New("callback argument missing")
			return toJSError(err)
		}
		cb := args[0]
		go func() {
			for {
				msg := <-m.Result
				cb.Invoke(fromResult(msg))
			}
		}()
		return nil
	})
	return map[string]interface{}{
		"data": dataFn,
	}
}
