package valid

import (
	"context"
	"errors"
	"reflect"
	"time"
)

const (
	tagName               = "validate"
	utf8HexComma          = "0x2C"
	utf8Pipe              = "0x7C"
	tagSeparator          = ","
	orSeparator           = "|"
	tagKeySeparator       = "="
	structOnlyTag         = "structonly"
	noStructLevelTag      = "nostructlevel"
	omitempty             = "omitempty"
	isdefault             = "isdefault"
	requiredWithoutAllTag = "required_without_all"
	requiredWithoutTag    = "required_without"
	requiredWithTag       = "required_with"
	requiredWithAllTag    = "required_with_all"
	requiredIfTag         = "required_if"
	requiredUnlessTag     = "required_unless"
	excludedWithoutAllTag = "excluded_without_all"
	excludedWithoutTag    = "excluded_without"
	excludedWithTag       = "excluded_with"
	excludedWithAllTag    = "excluded_with_all"
	excludedIfTag         = "excluded_if"
	excludedUnlessTag     = "excluded_unless"
	skipValidationTag     = "-"
	diveTag               = "dive"
	keysTag               = "keys"
	endKeysTag            = "endkeys"
	requiredTag           = "required"
	namespaceSeparator    = "."
	leftBracket           = "["
	rightBracket          = "]"
	restrictedTagChars    = ".[],|=+()`~!@#$%^&*\\\"/?<>{}"
	restrictedAliasErr    = "Alias '%s' either contains restricted characters or is the same as a restricted tag needed for normal operation"
	restrictedTagErr      = "Tag '%s' either contains restricted characters or is the same as a restricted tag needed for normal operation"
)

var (
	timeDurationType = reflect.TypeOf(time.Duration(0))
	timeType         = reflect.TypeOf(time.Time{})

	defaultCField = &cField{namesEqual: true}
)

// FilterFunc is the type used to filter fields using
// StructFiltered(...) function.
// returning true results in the field being filtered/skiped from
// validation
type FilterFunc func(ns []byte) bool

// CustomTypeFunc allows for overriding or adding custom field type handler functions
// field = field value of the type to return a value to be validated
// example Valuer from sql drive see https://golang.org/src/database/sql/driver/types.go?s=1210:1293#L29
type CustomTypeFunc func(field reflect.Value) interface{}

// TagNameFunc allows for adding of a custom tag name parser
type TagNameFunc func(field reflect.StructField) string

type internalValidationFuncWrapper struct {
	fn                FuncCtx
	runValidatinOnNil bool
}

// Validate contains the validator settings and cache
type Validate struct {
	validations map[string]internalValidationFuncWrapper
	tagCache    map[string]*cTag
}

// New returns a new instance of 'validate' with sane defaults.
// Validate is designed to be thread-safe and used as a singleton instance.
// It caches information about your struct and validations,
// in essence only parsing your validation tags once per struct type.
// Using multiple instances neglects the benefit of caching.
func New() *Validate {
	v := &Validate{
		validations: make(map[string]internalValidationFuncWrapper, len(bakedInValidators)),
		tagCache:    make(map[string]*cTag, 3),
	}
	// // must copy validators for separate validations to be used in each instance
	for k, val := range bakedInValidators {
		switch k {
		// these require that even if the value is nil that the validation should run, omitempty still overrides this behaviour
		case requiredIfTag, requiredUnlessTag, requiredWithTag, requiredWithAllTag, requiredWithoutTag, requiredWithoutAllTag,
			excludedIfTag, excludedUnlessTag, excludedWithTag, excludedWithAllTag, excludedWithoutTag, excludedWithoutAllTag:
			_ = v.registerValidation(k, wrapFunc(val), true, true)
		default:
			// no need to error check here, baked in will always be valid
			_ = v.registerValidation(k, wrapFunc(val), true, false)
		}
	}
	return v
}

// ValidateMapCtx validates a map using a map of validation rules and allows passing of contextual
// validation validation information via context.Context.
func (v Validate) ValidateMap(ctx context.Context, data map[string]interface{}, rules map[string]interface{}) map[string]error {
	var errs map[string]error

	for field, rule := range rules {
		if ruleStr, ok := rule.(string); ok {
			err := v.VarCtx(ctx, data[field], ruleStr)
			if err != nil {
				if errs == nil {
					errs = make(map[string]error)
				}
				errs[field] = err
			}
		}
	}
	return errs
}

// Var validates a single variable using tag style validation.
// eg.
// var i int
// validate.Var(i, "gt=1,lt=10")
//
// WARNING: a struct can be passed for validation eg. time.Time is a struct or
// if you have a custom type and have registered a custom type handler, so must
// allow it; however unforeseen validations will occur if trying to validate a
// struct that is meant to be passed to 'validate.Struct'
//
// It returns InvalidValidationError for bad values passed in and nil or ValidationErrors as error otherwise.
// You will need to assert the error if it's not nil eg. err.(validator.ValidationErrors) to access the array of errors.
// validate Array, Slice and maps fields which may contain more than one error
func (v *Validate) Var(field interface{}, tag string) error {
	return v.VarCtx(context.Background(), field, tag)
}

// VarCtx validates a single variable using tag style validation and allows passing of contextual
// validation validation information via context.Context.
// eg.
// var i int
// validate.Var(i, "gt=1,lt=10")
//
// WARNING: a struct can be passed for validation eg. time.Time is a struct or
// if you have a custom type and have registered a custom type handler, so must
// allow it; however unforeseen validations will occur if trying to validate a
// struct that is meant to be passed to 'validate.Struct'
//
// It returns InvalidValidationError for bad values passed in and nil or ValidationErrors as error otherwise.
// You will need to assert the error if it's not nil eg. err.(validator.ValidationErrors) to access the array of errors.
// validate Array, Slice and maps fields which may contain more than one error
func (v *Validate) VarCtx(ctx context.Context, field interface{}, tag string) (err error) {
	if len(tag) == 0 || tag == skipValidationTag {
		return nil
	}

	ctag := v.fetchCacheTag(tag)
	val := reflect.ValueOf(field)

	vd := &validate{}
	vd.top = val
	vd.isPartial = false
	err = vd.traverseField(ctx, val, val, vd.ns[0:0], vd.actualNs[0:0], defaultCField, ctag)
	return
}

// VarWithValue validates a single variable, against another variable/field's value using tag style validation
// eg.
// s1 := "abcd"
// s2 := "abcd"
// validate.VarWithValue(s1, s2, "eqcsfield") // returns true
//
// WARNING: a struct can be passed for validation eg. time.Time is a struct or
// if you have a custom type and have registered a custom type handler, so must
// allow it; however unforeseen validations will occur if trying to validate a
// struct that is meant to be passed to 'validate.Struct'
//
// It returns InvalidValidationError for bad values passed in and nil or ValidationErrors as error otherwise.
// You will need to assert the error if it's not nil eg. err.(validator.ValidationErrors) to access the array of errors.
// validate Array, Slice and maps fields which may contain more than one error
func (v *Validate) VarWithValue(field interface{}, other interface{}, tag string) error {
	return v.VarWithValueCtx(context.Background(), field, other, tag)
}

// VarWithValueCtx validates a single variable, against another variable/field's value using tag style validation and
// allows passing of contextual validation validation information via context.Context.
// eg.
// s1 := "abcd"
// s2 := "abcd"
// validate.VarWithValue(s1, s2, "eqcsfield") // returns true
//
// WARNING: a struct can be passed for validation eg. time.Time is a struct or
// if you have a custom type and have registered a custom type handler, so must
// allow it; however unforeseen validations will occur if trying to validate a
// struct that is meant to be passed to 'validate.Struct'
//
// It returns InvalidValidationError for bad values passed in and nil or ValidationErrors as error otherwise.
// You will need to assert the error if it's not nil eg. err.(validator.ValidationErrors) to access the array of errors.
// validate Array, Slice and maps fields which may contain more than one error
func (v *Validate) VarWithValueCtx(ctx context.Context, field interface{}, other interface{}, tag string) (err error) {
	if len(tag) == 0 || tag == skipValidationTag {
		return nil
	}
	ctag := v.fetchCacheTag(tag)
	otherVal := reflect.ValueOf(other)

	vd := &validate{}
	vd.top = otherVal
	vd.isPartial = false
	err = vd.traverseField(ctx, otherVal, reflect.ValueOf(field), vd.ns[0:0], vd.actualNs[0:0], defaultCField, ctag)
	return
}

func (v *Validate) registerValidation(tag string, fn FuncCtx, bakedIn bool, nilCheckable bool) error {
	if len(tag) == 0 {
		return errors.New("function Key cannot be empty")
	}
	if fn == nil {
		return errors.New("function cannot be empty")
	}
	v.validations[tag] = internalValidationFuncWrapper{fn: fn, runValidatinOnNil: nilCheckable}
	return nil
}
