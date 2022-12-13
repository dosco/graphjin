//go:build !wasm

package core

import (
	"github.com/dosco/graphjin/plugin/js"
)

func (gj *graphjin) initScript() (err error) {
	gj.scriptMap[".js"] = js.New()
	return
}
