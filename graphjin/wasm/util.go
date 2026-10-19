//go:build wasm && js

package main

import (
	"errors"
	"fmt"
	"syscall/js"
)

func await(awaitable js.Value) ([]js.Value, []js.Value) {
	then := make(chan []js.Value)
	defer close(then)
	thenFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		then <- args
		return nil
	})
	defer thenFunc.Release()

	catch := make(chan []js.Value)
	defer close(catch)
	catchFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		catch <- args
		return nil
	})
	defer catchFunc.Release()

	awaitable.Call("then", thenFunc).Call("catch", catchFunc)

	select {
	case result := <-then:
		return result, nil
	case err := <-catch:
		return nil, err
	}
}

func toAwait(fn func(resolve, reject js.Value)) js.Value {
	h := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resolve := args[0]
		reject := args[1]
		go fn(resolve, reject)
		return nil
	})
	return js.Global().Get("Promise").New(h)
}

func toError(err interface{}) error {
	if v, ok := err.(js.Error); ok {
		return errors.New(v.Get("message").String())
	}
	return nil
}

func toJSError(err error) js.Value {
	return js.Global().Get("Error").New(err.Error())
}

func debug(v js.Value) {
	fmt.Println(js.Global().Get("JSON").Call("stringify", v))
}
