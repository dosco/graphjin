//go:build wasm && js

package main

import (
	"encoding/json"
	"errors"
	"syscall/js"
)

type queryArgs struct {
	conn   js.Value
	userID interface{}
	query  string
	vars   json.RawMessage
}

// func processArgsWithTx(qa *queryArgs, args []js.Value, argName string) (jsErr js.Value) {
// 	if len(args) < 1 {
// 		jsErr = toJSError(errors.New("required argument: transaction/connection"))
// 		return
// 	}
// 	conn := args[0]

// 	if conn.Type() != js.TypeObject {
// 		jsErr = toJSError(errors.New("argument missing: transaction/connection"))
// 		return
// 	}
// 	qa.conn = conn

// 	return processArgs(qa, args[1:], argName)
// }

func processArgs(qa *queryArgs, args []js.Value, argName string) (err js.Value) {
	if len(args) < 1 {
		err = toJSError(errors.New("required argument: " + argName))
		return
	}
	query := args[0]

	if query.Type() != js.TypeString || query.String() == "" {
		err = toJSError(errors.New("argument missing: " + argName))
		return
	}
	qa.query = query.String()

	return processCommonArgs(qa, args[1:])
}

func processCommonArgs(qa *queryArgs, args []js.Value) (err js.Value) {
	if len(args) == 0 {
		return
	}
	vars := args[0]

	if vars.Type() != js.TypeObject &&
		vars.Type() != js.TypeNull &&
		vars.Type() != js.TypeUndefined {
		err = toJSError(
			errors.New("variables argument can only be a string or null"))
		return
	}

	if vars.Type() == js.TypeObject {
		val := js.Global().Get("JSON").Call("stringify", vars)
		qa.vars = json.RawMessage(val.String())
	}

	if len(args) == 1 {
		return
	}
	opts := args[1]

	if opts.Type() != js.TypeObject &&
		opts.Type() != js.TypeNull &&
		opts.Type() != js.TypeUndefined {
		err = toJSError(
			errors.New("options argument can only be a object or null"))
		return
	}

	if v := opts.Get("userID"); v.Type() == js.TypeString || v.Type() == js.TypeNumber {
		qa.userID = optVal(v)
	}
	return
}

// func toTx(dbType string, val js.Value) *sql.Tx {
// 	switch dbType {
// 	case "mysql":
// 		return sql.Tx(&MyConn{client: val})
// 	default:
// 		return = sql.Tx(&MyConn{client: val})
// 	}
// }

func optVal(val js.Value) interface{} {
	switch val.Type() {
	case js.TypeString:
		return val.String()
	case js.TypeNumber:
		return val.Int()
	case js.TypeBoolean:
		return val.Bool()
	default:
		return js.Null
	}
}
