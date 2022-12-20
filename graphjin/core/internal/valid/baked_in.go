package valid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Func accepts a FieldLevel interface for all validation needs. The return
// value should be true when validation succeeds.
type Func func(fl FieldLevel) bool

// FuncCtx accepts a context.Context and FieldLevel interface for all
// validation needs. The return value should be true when validation succeeds.
type FuncCtx func(ctx context.Context, fl FieldLevel) bool

// wrapFunc wraps noramal Func makes it compatible with FuncCtx
func wrapFunc(fn Func) FuncCtx {
	if fn == nil {
		return nil // be sure not to wrap a bad function.
	}
	return func(ctx context.Context, fl FieldLevel) bool {
		return fn(fl)
	}
}

var (
	// bakedInValidators is the default map of ValidationFunc
	// you can add, remove or even replace items to suite your needs,
	// or even disregard and use your own map if so desired.
	bakedInValidators = map[string]Func{
		"required":             hasValue,
		"required_if":          requiredIf,
		"required_unless":      requiredUnless,
		"required_with":        requiredWith,
		"required_with_all":    requiredWithAll,
		"required_without":     requiredWithout,
		"required_without_all": requiredWithoutAll,
		"excluded_if":          excludedIf,
		"excluded_unless":      excludedUnless,
		"excluded_with":        excludedWith,
		"excluded_with_all":    excludedWithAll,
		"excluded_without":     excludedWithout,
		"excluded_without_all": excludedWithoutAll,
		"isdefault":            isDefault,
		"len":                  hasLengthOf,
		"min":                  hasMinOf,
		"max":                  hasMaxOf,
		"eq":                   isEq,
		"ne":                   isNe,
		"lt":                   isLt,
		"lte":                  isLte,
		"gt":                   isGt,
		"gte":                  isGte,
		"nefield":              isNeField,
		"gtefield":             isGteField,
		"gtfield":              isGtField,
		"ltefield":             isLteField,
		"ltfield":              isLtField,
		"fieldcontains":        fieldContains,
		"fieldexcludes":        fieldExcludes,
		"alpha":                isAlpha,
		"alphanum":             isAlphanum,
		"alphaunicode":         isAlphaUnicode,
		"alphanumunicode":      isAlphanumUnicode,
		"boolean":              isBoolean,
		"numeric":              isNumeric,
		"number":               isNumber,
		"email":                isEmail,
		"url":                  isURL,
		"uri":                  isURI,
		"contains":             contains,
		"containsany":          containsAny,
		"containsrune":         containsRune,
		"excludes":             excludes,
		"excludesall":          excludesAll,
		"excludesrune":         excludesRune,
		"startswith":           startsWith,
		"endswith":             endsWith,
		"startsnotwith":        startsNotWith,
		"endsnotwith":          endsNotWith,
		"uuid":                 isUUID,
		"uuid3":                isUUID3,
		"uuid4":                isUUID4,
		"uuid5":                isUUID5,
		"uuid_rfc4122":         isUUIDRFC4122,
		"uuid3_rfc4122":        isUUID3RFC4122,
		"uuid4_rfc4122":        isUUID4RFC4122,
		"uuid5_rfc4122":        isUUID5RFC4122,
		"ulid":                 isULID,
		"ascii":                isASCII,
		"printascii":           isPrintableASCII,
		"multibyte":            hasMultiByteCharacter,
		"oneof":                isOneOf,
		"url_encoded":          isURLEncoded,
		"json":                 isJSON,
		"lowercase":            isLowercase,
		"uppercase":            isUppercase,
		"datetime":             isDatetime,
		"timezone":             isTimeZone,
	}
)

var (
	oneofValsCache = map[string][]string{}
)

func parseOneOfParam2(s string) []string {
	vals, ok := oneofValsCache[s]
	if !ok {
		vals = splitParamsRegex.FindAllString(s, -1)
		for i := 0; i < len(vals); i++ {
			vals[i] = strings.Replace(vals[i], "'", "", -1)
		}
		oneofValsCache[s] = vals
	}
	return vals
}

func isURLEncoded(fl FieldLevel) bool {
	return uRLEncodedRegex.MatchString(fl.Field().String())
}

func isOneOf(fl FieldLevel) bool {
	vals := parseOneOfParam2(fl.Param())

	field := fl.Field()

	var v string
	switch field.Kind() {
	case reflect.String:
		v = field.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v = strconv.FormatInt(field.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v = strconv.FormatUint(field.Uint(), 10)
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}
	for i := 0; i < len(vals); i++ {
		if vals[i] == v {
			return true
		}
	}
	return false
}

// hasMultiByteCharacter is the validation function for validating if the field's value has a multi byte character.
func hasMultiByteCharacter(fl FieldLevel) bool {
	field := fl.Field()

	if field.Len() == 0 {
		return true
	}

	return multibyteRegex.MatchString(field.String())
}

// isPrintableASCII is the validation function for validating if the field's value is a valid printable ASCII character.
func isPrintableASCII(fl FieldLevel) bool {
	return printableASCIIRegex.MatchString(fl.Field().String())
}

// isASCII is the validation function for validating if the field's value is a valid ASCII character.
func isASCII(fl FieldLevel) bool {
	return aSCIIRegex.MatchString(fl.Field().String())
}

// isUUID5 is the validation function for validating if the field's value is a valid v5 UUID.
func isUUID5(fl FieldLevel) bool {
	return uUID5Regex.MatchString(fl.Field().String())
}

// isUUID4 is the validation function for validating if the field's value is a valid v4 UUID.
func isUUID4(fl FieldLevel) bool {
	return uUID4Regex.MatchString(fl.Field().String())
}

// isUUID3 is the validation function for validating if the field's value is a valid v3 UUID.
func isUUID3(fl FieldLevel) bool {
	return uUID3Regex.MatchString(fl.Field().String())
}

// isUUID is the validation function for validating if the field's value is a valid UUID of any version.
func isUUID(fl FieldLevel) bool {
	return uUIDRegex.MatchString(fl.Field().String())
}

// isUUID5RFC4122 is the validation function for validating if the field's value is a valid RFC4122 v5 UUID.
func isUUID5RFC4122(fl FieldLevel) bool {
	return uUID5RFC4122Regex.MatchString(fl.Field().String())
}

// isUUID4RFC4122 is the validation function for validating if the field's value is a valid RFC4122 v4 UUID.
func isUUID4RFC4122(fl FieldLevel) bool {
	return uUID4RFC4122Regex.MatchString(fl.Field().String())
}

// isUUID3RFC4122 is the validation function for validating if the field's value is a valid RFC4122 v3 UUID.
func isUUID3RFC4122(fl FieldLevel) bool {
	return uUID3RFC4122Regex.MatchString(fl.Field().String())
}

// isUUIDRFC4122 is the validation function for validating if the field's value is a valid RFC4122 UUID of any version.
func isUUIDRFC4122(fl FieldLevel) bool {
	return uUIDRFC4122Regex.MatchString(fl.Field().String())
}

// isULID is the validation function for validating if the field's value is a valid ULID.
func isULID(fl FieldLevel) bool {
	return uLIDRegex.MatchString(fl.Field().String())
}

// excludesRune is the validation function for validating that the field's value does not contain the rune specified within the param.
func excludesRune(fl FieldLevel) bool {
	return !containsRune(fl)
}

// excludesAll is the validation function for validating that the field's value does not contain any of the characters specified within the param.
func excludesAll(fl FieldLevel) bool {
	return !containsAny(fl)
}

// excludes is the validation function for validating that the field's value does not contain the text specified within the param.
func excludes(fl FieldLevel) bool {
	return !contains(fl)
}

// containsRune is the validation function for validating that the field's value contains the rune specified within the param.
func containsRune(fl FieldLevel) bool {
	r, _ := utf8.DecodeRuneInString(fl.Param())

	return strings.ContainsRune(fl.Field().String(), r)
}

// containsAny is the validation function for validating that the field's value contains any of the characters specified within the param.
func containsAny(fl FieldLevel) bool {
	return strings.ContainsAny(fl.Field().String(), fl.Param())
}

// contains is the validation function for validating that the field's value contains the text specified within the param.
func contains(fl FieldLevel) bool {
	return strings.Contains(fl.Field().String(), fl.Param())
}

// startsWith is the validation function for validating that the field's value starts with the text specified within the param.
func startsWith(fl FieldLevel) bool {
	return strings.HasPrefix(fl.Field().String(), fl.Param())
}

// endsWith is the validation function for validating that the field's value ends with the text specified within the param.
func endsWith(fl FieldLevel) bool {
	return strings.HasSuffix(fl.Field().String(), fl.Param())
}

// startsNotWith is the validation function for validating that the field's value does not start with the text specified within the param.
func startsNotWith(fl FieldLevel) bool {
	return !startsWith(fl)
}

// endsNotWith is the validation function for validating that the field's value does not end with the text specified within the param.
func endsNotWith(fl FieldLevel) bool {
	return !endsWith(fl)
}

// fieldContains is the validation function for validating if the current field's value contains the field specified by the param's value.
func fieldContains(fl FieldLevel) bool {
	field := fl.Field()
	currentField, _, ok := fl.GetStructFieldOK()
	if !ok {
		return false
	}
	return strings.Contains(field.String(), currentField.String())
}

// fieldExcludes is the validation function for validating if the current field's value excludes the field specified by the param's value.
func fieldExcludes(fl FieldLevel) bool {
	field := fl.Field()
	currentField, _, ok := fl.GetStructFieldOK()
	if !ok {
		return true
	}
	return !strings.Contains(field.String(), currentField.String())
}

// isNeField is the validation function for validating if the current field's value is not equal to the field specified by the param's value.
func isNeField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()

	if !ok || currentKind != kind {
		return true
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() != currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() != currentField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() != currentField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) != int64(currentField.Len())

	case reflect.Bool:
		return field.Bool() != currentField.Bool()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Interface().(time.Time)
			fieldTime := field.Interface().(time.Time)

			return !fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return true
		}
	}

	// default reflect.String:
	return field.String() != currentField.String()
}

// isNe is the validation function for validating that the field's value does not equal the provided param value.
func isNe(fl FieldLevel) bool {
	return !isEq(fl)
}

// isEq is the validation function for validating if the current field's value is equal to the param's value.
func isEq(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		return field.String() == param

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) == p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() == p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() == p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() == p

	case reflect.Bool:
		p := asBool(param)

		return field.Bool() == p
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isURI is the validation function for validating if the current field's value is a valid URI.
func isURI(fl FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.String:

		s := field.String()

		// checks needed as of Go 1.6 because of change https://github.com/golang/go/commit/617c93ce740c3c3cc28cdd1a0d712be183d0b328#diff-6c2d018290e298803c0c9419d8739885L195
		// emulate browser and strip the '#' suffix prior to validation. see issue-#237
		if i := strings.Index(s, "#"); i > -1 {
			s = s[:i]
		}

		if len(s) == 0 {
			return false
		}

		_, err := url.ParseRequestURI(s)

		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isURL is the validation function for validating if the current field's value is a valid URL.
func isURL(fl FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.String:

		var i int
		s := field.String()

		// checks needed as of Go 1.6 because of change https://github.com/golang/go/commit/617c93ce740c3c3cc28cdd1a0d712be183d0b328#diff-6c2d018290e298803c0c9419d8739885L195
		// emulate browser and strip the '#' suffix prior to validation. see issue-#237
		if i = strings.Index(s, "#"); i > -1 {
			s = s[:i]
		}

		if len(s) == 0 {
			return false
		}

		url, err := url.ParseRequestURI(s)

		if err != nil || url.Scheme == "" {
			return false
		}

		return true
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isEmail is the validation function for validating if the current field's value is a valid email address.
func isEmail(fl FieldLevel) bool {
	return emailRegex.MatchString(fl.Field().String())
}

// isNumber is the validation function for validating if the current field's value is a valid number.
func isNumber(fl FieldLevel) bool {
	switch fl.Field().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64:
		return true
	default:
		return numberRegex.MatchString(fl.Field().String())
	}
}

// isNumeric is the validation function for validating if the current field's value is a valid numeric value.
func isNumeric(fl FieldLevel) bool {
	switch fl.Field().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64:
		return true
	default:
		return numericRegex.MatchString(fl.Field().String())
	}
}

// isAlphanum is the validation function for validating if the current field's value is a valid alphanumeric value.
func isAlphanum(fl FieldLevel) bool {
	return alphaNumericRegex.MatchString(fl.Field().String())
}

// isAlpha is the validation function for validating if the current field's value is a valid alpha value.
func isAlpha(fl FieldLevel) bool {
	return alphaRegex.MatchString(fl.Field().String())
}

// isAlphanumUnicode is the validation function for validating if the current field's value is a valid alphanumeric unicode value.
func isAlphanumUnicode(fl FieldLevel) bool {
	return alphaUnicodeNumericRegex.MatchString(fl.Field().String())
}

// isAlphaUnicode is the validation function for validating if the current field's value is a valid alpha unicode value.
func isAlphaUnicode(fl FieldLevel) bool {
	return alphaUnicodeRegex.MatchString(fl.Field().String())
}

// isBoolean is the validation function for validating if the current field's value is a valid boolean value or can be safely converted to a boolean value.
func isBoolean(fl FieldLevel) bool {
	switch fl.Field().Kind() {
	case reflect.Bool:
		return true
	default:
		_, err := strconv.ParseBool(fl.Field().String())
		return err == nil
	}
}

// isDefault is the opposite of required aka hasValue
func isDefault(fl FieldLevel) bool {
	return !hasValue(fl)
}

// hasValue is the validation function for validating if the current field's value is not the default static value.
func hasValue(fl FieldLevel) bool {
	field := fl.Field()
	switch field.Kind() {
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func:
		return !field.IsNil()
	default:
		if fl.(*validate).fldIsPointer && field.Interface() != nil {
			return true
		}
		return field.IsValid() && field.Interface() != reflect.Zero(field.Type()).Interface()
	}
}

// requireCheckField is a func for check field kind
func requireCheckFieldKind(fl FieldLevel, param string, defaultNotFoundValue bool) bool {
	field := fl.Field()
	kind := field.Kind()
	var nullable, found bool
	if len(param) > 0 {
		field, kind, nullable, found = fl.GetStructFieldOKAdvanced2(fl.Parent(), param)
		if !found {
			return defaultNotFoundValue
		}
	}
	switch kind {
	case reflect.Invalid:
		return defaultNotFoundValue
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func:
		return field.IsNil()
	default:
		if nullable && field.Interface() != nil {
			return false
		}
		return field.IsValid() && field.Interface() == reflect.Zero(field.Type()).Interface()
	}
}

// requireCheckFieldValue is a func for check field value
func requireCheckFieldValue(fl FieldLevel, param string, value string, defaultNotFoundValue bool) bool {
	field, kind, _, found := fl.GetStructFieldOKAdvanced2(fl.Parent(), param)
	if !found {
		return defaultNotFoundValue
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() == asInt(value)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() == asUint(value)

	case reflect.Float32, reflect.Float64:
		return field.Float() == asFloat(value)

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) == asInt(value)

	case reflect.Bool:
		return field.Bool() == asBool(value)
	}

	// default reflect.String:
	return field.String() == value
}

// requiredIf is the validation function
// The field under validation must be present and not empty only if all the other specified fields are equal to the value following with the specified field.
func requiredIf(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for required_if %s", fl.FieldName()))
	}
	for i := 0; i < len(params); i += 2 {
		if !requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return true
		}
	}
	return hasValue(fl)
}

// excludedIf is the validation function
// The field under validation must not be present or is empty only if all the other specified fields are equal to the value following with the specified field.
func excludedIf(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for excluded_if %s", fl.FieldName()))
	}

	for i := 0; i < len(params); i += 2 {
		if !requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return false
		}
	}
	return true
}

// requiredUnless is the validation function
// The field under validation must be present and not empty only unless all the other specified fields are equal to the value following with the specified field.
func requiredUnless(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for required_unless %s", fl.FieldName()))
	}

	for i := 0; i < len(params); i += 2 {
		if requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return true
		}
	}
	return hasValue(fl)
}

// excludedUnless is the validation function
// The field under validation must not be present or is empty unless all the other specified fields are equal to the value following with the specified field.
func excludedUnless(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for excluded_unless %s", fl.FieldName()))
	}
	for i := 0; i < len(params); i += 2 {
		if !requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return true
		}
	}
	return !hasValue(fl)
}

// excludedWith is the validation function
// The field under validation must not be present or is empty if any of the other specified fields are present.
func excludedWith(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return !hasValue(fl)
		}
	}
	return true
}

// requiredWith is the validation function
// The field under validation must be present and not empty only if any of the other specified fields are present.
func requiredWith(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return hasValue(fl)
		}
	}
	return true
}

// excludedWithAll is the validation function
// The field under validation must not be present or is empty if all of the other specified fields are present.
func excludedWithAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return !hasValue(fl)
}

// requiredWithAll is the validation function
// The field under validation must be present and not empty only if all of the other specified fields are present.
func requiredWithAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return hasValue(fl)
}

// excludedWithout is the validation function
// The field under validation must not be present or is empty when any of the other specified fields are not present.
func excludedWithout(fl FieldLevel) bool {
	if requireCheckFieldKind(fl, strings.TrimSpace(fl.Param()), true) {
		return !hasValue(fl)
	}
	return true
}

// requiredWithout is the validation function
// The field under validation must be present and not empty only when any of the other specified fields are not present.
func requiredWithout(fl FieldLevel) bool {
	if requireCheckFieldKind(fl, strings.TrimSpace(fl.Param()), true) {
		return hasValue(fl)
	}
	return true
}

// excludedWithoutAll is the validation function
// The field under validation must not be present or is empty when all of the other specified fields are not present.
func excludedWithoutAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return !hasValue(fl)
}

// requiredWithoutAll is the validation function
// The field under validation must be present and not empty only when all of the other specified fields are not present.
func requiredWithoutAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return hasValue(fl)
}

// isGteField is the validation function for validating if the current field's value is greater than or equal to the field specified by the param's value.
func isGteField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() >= currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() >= currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() >= currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.After(t) || fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) >= len(currentField.String())
}

// isGtField is the validation function for validating if the current field's value is greater than the field specified by the param's value.
func isGtField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() > currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() > currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() > currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.After(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) > len(currentField.String())
}

// isGte is the validation function for validating if the current field's value is greater than or equal to the param's value.
func isGte(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) >= p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) >= p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() >= p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() >= p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() >= p

	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			now := time.Now().UTC()
			t := field.Convert(timeType).Interface().(time.Time)

			return t.After(now) || t.Equal(now)
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isGt is the validation function for validating if the current field's value is greater than the param's value.
func isGt(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) > p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) > p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() > p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() > p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() > p
	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			return field.Convert(timeType).Interface().(time.Time).After(time.Now().UTC())
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// hasLengthOf is the validation function for validating if the current field's value is equal to the param's value.
func hasLengthOf(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) == p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) == p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() == p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() == p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() == p
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// hasMinOf is the validation function for validating if the current field's value is greater than or equal to the param's value.
func hasMinOf(fl FieldLevel) bool {
	return isGte(fl)
}

// isLteField is the validation function for validating if the current field's value is less than or equal to the field specified by the param's value.
func isLteField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() <= currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() <= currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() <= currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.Before(t) || fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) <= len(currentField.String())
}

// isLtField is the validation function for validating if the current field's value is less than the field specified by the param's value.
func isLtField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() < currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() < currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() < currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.Before(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) < len(currentField.String())
}

// isLte is the validation function for validating if the current field's value is less than or equal to the param's value.
func isLte(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) <= p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) <= p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() <= p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() <= p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() <= p

	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			now := time.Now().UTC()
			t := field.Convert(timeType).Interface().(time.Time)

			return t.Before(now) || t.Equal(now)
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isLt is the validation function for validating if the current field's value is less than the param's value.
func isLt(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) < p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) < p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() < p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() < p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() < p

	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			return field.Convert(timeType).Interface().(time.Time).Before(time.Now().UTC())
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// hasMaxOf is the validation function for validating if the current field's value is less than or equal to the param's value.
func hasMaxOf(fl FieldLevel) bool {
	return isLte(fl)
}

// isJSON is the validation function for validating if the current field's value is a valid json string.
func isJSON(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		val := field.String()
		return json.Valid([]byte(val))
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isLowercase is the validation function for validating if the current field's value is a lowercase string.
func isLowercase(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		if field.String() == "" {
			return false
		}
		return field.String() == strings.ToLower(field.String())
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isUppercase is the validation function for validating if the current field's value is an uppercase string.
func isUppercase(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		if field.String() == "" {
			return false
		}
		return field.String() == strings.ToUpper(field.String())
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isDatetime is the validation function for validating if the current field's value is a valid datetime string.
func isDatetime(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	if field.Kind() == reflect.String {
		_, err := time.Parse(param, field.String())

		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isTimeZone is the validation function for validating if the current field's value is a valid time zone string.
func isTimeZone(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		// empty value is converted to UTC by time.LoadLocation but disallow it as it is not a valid time zone name
		if field.String() == "" {
			return false
		}

		// Local value is converted to the current system time zone by time.LoadLocation but disallow it as it is not a valid time zone name
		if strings.ToLower(field.String()) == "local" {
			return false
		}

		_, err := time.LoadLocation(field.String())
		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}
