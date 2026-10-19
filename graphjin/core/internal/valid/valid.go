package valid

import (
	"bytes"
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

const formatsEnum = "validateFormatEnum"

var Validators = map[string]qcode.Validator{
	"format": {
		Description: "Value must be of a format, eg: email, uuid",
		Types:       []graph.ParserType{graph.NodeLabel, graph.NodeStr},
		Type:        formatsEnum,
		NewFn:       format,
	},
	"required": {
		Description: "Variable is required",
		Types:       []graph.ParserType{graph.NodeBool},
		Type:        "Boolean",
		NewFn:       required,
	},
	"requiredIf": {
		Description: "Variable is required if another variable equals a value",
		Types:       []graph.ParserType{graph.NodeObj},
		NewFn:       requiredIf,
	},
	"requiredUnless": {
		Description: "Variable is required unless another variable equals a value",
		Types:       []graph.ParserType{graph.NodeObj},
		NewFn:       requiredUnless,
	},
	"requiredWith": {
		Description: "Variable is required if one of a list of other variables exist",
		Types:       []graph.ParserType{graph.NodeList, graph.NodeStr},
		Type:        "String",
		List:        true,
		NewFn:       requiredWith,
	},
	"requiredWithAll": {
		Description: "Variable is required if all of a list of other variables exist",
		Types:       []graph.ParserType{graph.NodeList, graph.NodeStr},
		Type:        "String",
		List:        true,
		NewFn:       requiredWithAll,
	},
	"requiredWithout": {
		Description: "Variable is required if one of a list of other variables does not exist",
		Types:       []graph.ParserType{graph.NodeList, graph.NodeStr},
		Type:        "String",
		List:        true,
		NewFn:       requiredWithout,
	},
	"requiredWithoutAll": {
		Description: "Variable is required if none of a list of other variables exist",
		Types:       []graph.ParserType{graph.NodeList, graph.NodeStr},
		Type:        "String",
		List:        true,
		NewFn:       requiredWithoutAll,
	},
	"max": {
		Description: "Maximum value a variable can be",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "Int",
		NewFn:       max,
	},
	"min": {
		Description: "Minimum value a variable can be",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "Int",
		NewFn:       min,
	},
	"equals": {
		Description: "Variable equals a value",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "String",
		NewFn:       equals,
	},
	"notEquals": {
		Description: "Variable does not equal a value",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "String",
		NewFn:       notEquals,
	},
	"oneOf": {
		Description: "Variable equals one of the following values",
		Types: []graph.ParserType{
			graph.NodeList, graph.NodeNum, graph.NodeList, graph.NodeStr,
		},
		Type:  "String",
		List:  true,
		NewFn: oneOf,
	},
	"greaterThan": {
		Description: "Variable is greater than a value",
		Types:       []graph.ParserType{graph.NodeNum},
		Type:        "Int",
		NewFn:       greaterThan,
	},
	"greaterThanOrEquals": {
		Description: "Variable is greater than or equal to a value",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "Int",
		NewFn:       greaterThanOrEquals,
	},
	"lessThan": {
		Description: "Variable is less than a value",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "Int",
		NewFn:       lessThan,
	},
	"lessThanOrEquals": {
		Description: "Variable is less than or equal to a value",
		Types:       []graph.ParserType{graph.NodeStr, graph.NodeNum},
		Type:        "Int",
		NewFn:       lessThanOrEquals,
	},
	"equalsField": {
		Description: "Variable equals the value of another variable",
		Types:       []graph.ParserType{graph.NodeStr},
		Type:        "Int",
		NewFn:       equalsField,
	},
	"notEqualsField": {
		Description: "Variable does not equal the value of another variable",
		Types:       []graph.ParserType{graph.NodeStr},
		Type:        "Int",
		NewFn:       notEqualsField,
	},
	"greaterThanField": {
		Description: "Variable is greater than the value of another variable",
		Types:       []graph.ParserType{graph.NodeStr},
		Type:        "String",
		NewFn:       greaterThanField,
	},
	"greaterThanOrEqualsField": {
		Description: "Variable is greater than or equals the value of another variable",
		Types:       []graph.ParserType{graph.NodeStr},
		Type:        "String",
		NewFn:       greaterThanOrEqualsField,
	},
	"lessThanField": {
		Description: "Variable is less than the value of another variable",
		Types:       []graph.ParserType{graph.NodeStr},
		Type:        "String",
		NewFn:       lessThanField,
	},
	"lessThanOrEqualsField": {
		Description: "Variable is less than or equals the value of another variable",
		Types:       []graph.ParserType{graph.NodeStr},
		Type:        "String",
		NewFn:       lessThanOrEqualsField,
	},
}

func required(args []string) (fn qcode.ValidFn, err error) {
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		_, ok := vars[c.VarName]
		return ok
	}
	return
}

func requiredIf(args []string) (fn qcode.ValidFn, err error) {
	return requiredIfUnless(args, true)
}

func requiredUnless(args []string) (fn qcode.ValidFn, err error) {
	return requiredIfUnless(args, false)
}

func requiredIfUnless(args []string, isIf bool) (fn qcode.ValidFn, err error) {
	keys := make([]string, len(args)/2)
	values := make([][]byte, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		keys[i] = args[i]
		values[i] = []byte(args[(i + 1)])
	}
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		m := true
		for i, k := range keys {
			v1, ok := vars[k]
			if !ok {
				m = false
				break
			}
			if ok := bytes.Equal(v1, values[i]); !ok {
				m = false
				break
			}
		}
		if isIf && m { // required if matched
			_, ok := vars[c.VarName]
			return ok
		} else if !isIf && !m { // required unless matched
			_, ok := vars[c.VarName]
			return ok
		}
		return true // not required
	}
	return
}

func min(args []string) (fn qcode.ValidFn, err error) {
	return minMax(args, true)
}

func max(args []string) (fn qcode.ValidFn, err error) {
	return minMax(args, false)
}

func minMax(args []string, min bool) (fn qcode.ValidFn, err error) {
	val, err := strconv.Atoi(args[0])
	if err != nil {
		return
	}
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		v1, ok := vars[c.VarName]
		if !ok {
			return false
		}
		n, err := strconv.Atoi(string(v1))
		if err != nil {
			return false
		}
		if min {
			return (n >= val)
		} else {
			return (n <= val)
		}
	}
	return
}

func equals(args []string) (fn qcode.ValidFn, err error) {
	return equalsAndNotEquals(args, true, false)
}

func notEquals(args []string) (fn qcode.ValidFn, err error) {
	return equalsAndNotEquals(args, false, false)
}

func equalsField(args []string) (fn qcode.ValidFn, err error) {
	return equalsAndNotEquals(args, true, true)
}

func notEqualsField(args []string) (fn qcode.ValidFn, err error) {
	return equalsAndNotEquals(args, false, true)
}

func equalsAndNotEquals(args []string, equals bool, field bool) (fn qcode.ValidFn, err error) {
	var val []byte
	if !field {
		val = []byte(args[0])
	}
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		v1, ok := vars[c.VarName]
		if !ok {
			return false
		}
		if field {
			v2, ok := vars[args[0]]
			if !ok {
				return false
			}
			val = v2
		}
		if equals {
			return bytes.Equal(v1, val)
		} else {
			return !bytes.Equal(v1, val)
		}
	}
	return
}

func requiredWith(args []string) (fn qcode.ValidFn, err error) {
	return conditionalRequired(args, true, false)
}

func requiredWithAll(args []string) (fn qcode.ValidFn, err error) {
	return conditionalRequired(args, true, true)
}

func requiredWithout(args []string) (fn qcode.ValidFn, err error) {
	return conditionalRequired(args, false, false)
}

func requiredWithoutAll(args []string) (fn qcode.ValidFn, err error) {
	return conditionalRequired(args, false, true)
}

func conditionalRequired(args []string, with, all bool) (fn qcode.ValidFn, err error) {
	keys := make([]string, len(args)/2)
	values := make([][]byte, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		keys[i] = args[i]
		values[i] = []byte(args[(i + 1)])
	}
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		m := all // if all then m = true else m = false
		for _, k := range keys {
			_, ok := vars[k]
			if all && !ok {
				m = false
				break
			} else if !all && ok {
				m = true
				break
			}
		}
		// required if with one or all or required if without one or all
		if (with && m) || (!with && !m) {
			_, ok := vars[c.VarName]
			return ok
		}
		return true // not required
	}
	return
}

func oneOf(args []string) (fn qcode.ValidFn, err error) {
	var vals [][]byte
	for _, a := range args {
		vals = append(vals, []byte(a))
	}
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		v1, ok := vars[c.VarName]
		if !ok {
			return false
		}
		for _, a := range vals {
			if bytes.Equal(a, v1) {
				return true
			}
		}
		return false
	}
	return
}

func greaterThan(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, true, false, false)
}

func greaterThanOrEquals(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, true, true, false)
}

func lessThan(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, false, false, false)
}

func lessThanOrEquals(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, false, true, false)
}

func greaterThanField(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, true, false, true)
}

func greaterThanOrEqualsField(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, true, true, true)
}

func lessThanField(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, false, false, true)
}

func lessThanOrEqualsField(args []string) (fn qcode.ValidFn, err error) {
	return greaterAndLessThanAndOrEquals(args, false, true, true)
}

func greaterAndLessThanAndOrEquals(args []string, greater, equals, field bool) (fn qcode.ValidFn, err error) {
	var val int
	if !field {
		val, err = strconv.Atoi(args[0])
		if err != nil {
			return
		}
	}
	fn = func(vars qcode.Vars, c qcode.Constraint) bool {
		v1, ok := vars[c.VarName]
		if !ok {
			return false
		}
		if field {
			v2, ok := vars[args[0]]
			if !ok {
				return false
			}
			val, err = strconv.Atoi(string(v2))
			if err != nil {
				return false
			}
		}
		n, err := strconv.Atoi(string(v1))
		if err != nil {
			return false
		}
		switch {
		case greater && !equals:
			return n > val
		case greater && equals:
			return n >= val
		case !greater && !equals:
			return n < val
		case !greater && equals:
			return n <= val
		default:
			return false
		}
	}
	return
}
