package plugin

import (
	"context"
	"errors"
)

type ScriptCompiler interface {
	CompileScript(name, source string) (ScriptExecuter, error)
}

type GraphQLFn func(query string, vars map[string]interface{}, opt map[string]string) map[string]interface{}

type ScriptExecuter interface {
	HasRequestFn() bool
	RequestFn(c context.Context,
		vars map[string]interface{},
		role string,
		userID interface{},
		gfn GraphQLFn) map[string]interface{}

	HasResponseFn() bool
	ReponseFn(c context.Context,
		vars map[string]interface{},
		role string,
		userID interface{},
		gfn GraphQLFn) map[string]interface{}
}

type ValidationCompiler interface {
	CompileValidation(source string) (ValidationExecuter, error)
}

type ValidationExecuter interface {
	Validate(vars []byte) error
}

var ErrNotFound = errors.New("file not found")

type FS interface {
	CreateDir(path string) error
	ReadDir(path string) (dirList []FileInfo, err error)

	CreateFile(path string, data []byte) error
	ReadFile(path string) (data []byte, err error)

	Exists(path string) (exists bool, err error)
}

type FileInfo interface {
	Name() string
	IsDir() bool
}
