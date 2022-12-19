//go:build wasm && js

package main

import (
	"encoding/json"
	"errors"
	"syscall/js"
)

type queryArgs struct {
	userID interface{}
	query  string
	vars   json.RawMessage
}

func newQueryArgs(args []js.Value) (qa queryArgs, jsErr js.Value) {
	if len(args) < 1 {
		err := errors.New("required arguments: query")
		return qa, toJSError(err)
	}
	query := args[0]

	if query.Type() != js.TypeString || query.String() == "" {
		return qa, toJSError(errors.New("query argument missing"))
	}
	qa.query = query.String()

	return processQueryArgs(qa, args)
}

func newQueryByNameArgs(args []js.Value) (qa queryArgs, jsErr js.Value) {
	if len(args) < 1 {
		err := errors.New("required arguments: name")
		return qa, toJSError(err)
	}
	name := args[0]

	if name.Type() != js.TypeString || name.String() == "" {
		return qa, toJSError(errors.New("query argument missing"))
	}
	qa.query = name.String()

	return processQueryArgs(qa, args)
}

func processQueryArgs(qa queryArgs, args []js.Value) (queryArgs, js.Value) {
	if len(args) == 1 {
		return qa, js.Null()
	}
	vars := args[1]

	if vars.Type() != js.TypeObject &&
		vars.Type() != js.TypeNull &&
		vars.Type() != js.TypeUndefined {
		err := errors.New("variables argument can only be a string or null")
		return qa, toJSError(err)
	}

	if vars.Type() == js.TypeObject {
		val := js.Global().Get("JSON").Call("stringify", vars)
		qa.vars = json.RawMessage(val.String())
	}

	if len(args) == 2 {
		return qa, js.Null()
	}
	opts := args[2]

	if opts.Type() != js.TypeObject &&
		opts.Type() != js.TypeNull &&
		opts.Type() != js.TypeUndefined {
		err := errors.New("options argument can only be a object or null")
		return qa, toJSError(err)
	}

	if v := opts.Get("userID"); v.Type() == js.TypeString || v.Type() == js.TypeNumber {
		qa.userID = optVal(v)
	}

	return qa, js.Null()
}

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
