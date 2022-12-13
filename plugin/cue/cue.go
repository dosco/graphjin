package cue

import (
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cuejson "cuelang.org/go/encoding/json"
	"github.com/dosco/graphjin/plugin"
)

type CueEngine struct{}

func New() *CueEngine { return &CueEngine{} }

type CueScript struct {
	val cue.Value
	mu  sync.Mutex
}

func (cu *CueEngine) CompileValidation(source string) (plugin.ValidationExecuter, error) {
	cuec := cuecontext.New()
	val := cuec.CompileString(source)
	return &CueScript{val: val}, nil
}

func (cs *CueScript) Validate(vars []byte) error {
	// TODO: better error handling. it's not clear that error came from validation
	// aslo it needs to be able to parse in frontend,
	// ex: error:{kind:"validation",problem:"out of range",path:"input.id",shoud_be:"<5"}
	cs.mu.Lock()
	err := cuejson.Validate(vars, cs.val)
	cs.mu.Unlock()
	return err
}
