package valid

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
)

var errMsg = "failed on '%s'"

// per validate construct
type validate struct {
	v            *Validate
	top          reflect.Value
	ns           []byte
	actualNs     []byte
	slflParent   reflect.Value // StructLevel & FieldLevel
	slCurrent    reflect.Value // StructLevel & FieldLevel
	flField      reflect.Value // StructLevel & FieldLevel
	cf           *cField       // StructLevel & FieldLevel
	ct           *cTag         // StructLevel & FieldLevel
	misc         []byte        // misc reusable
	str1         string        // misc reusable
	str2         string        // misc reusable
	fldIsPointer bool          // StructLevel & FieldLevel
	isPartial    bool
}

// traverseField validates any field, be it a struct or single field, ensures it's validity and passes it along to be validated via it's tag options
func (v *validate) traverseField(ctx context.Context, parent reflect.Value, current reflect.Value, ns []byte, structNs []byte, cf *cField, ct *cTag) (err error) {
	var kind reflect.Kind

	current, kind, v.fldIsPointer = v.extractTypeInternal(current, false)
	switch kind {
	case reflect.Ptr, reflect.Interface, reflect.Invalid:
		if ct == nil {
			return
		}

		if ct.typeof == typeOmitEmpty || ct.typeof == typeIsDefault {
			return
		}

		if ct.hasTag {
			if kind == reflect.Invalid {
				v.str1 = string(append(ns, cf.altName...))
				v.str2 = v.str1
				err = fmt.Errorf(errMsg, ct.tag)
				return
			}

			v.str1 = string(append(ns, cf.altName...))
			v.str2 = v.str1
			if !ct.runValidationWhenNil {
				err = fmt.Errorf(errMsg, ct.tag)
				return
			}
		}
	}

	if ct == nil || !ct.hasTag {
		return
	}

OUTER:
	for {
		if ct == nil {
			return
		}

		switch ct.typeof {
		case typeOmitEmpty:
			// set Field Level fields
			v.slflParent = parent
			v.flField = current
			v.cf = cf
			v.ct = ct

			if !hasValue(v) {
				return
			}

			ct = ct.next
			continue

		case typeEndKeys:
			return

		case typeDive:
			ct = ct.next

			// traverse slice or map here
			// or panic ;)
			switch kind {
			case reflect.Slice, reflect.Array:

				var i64 int64
				reusableCF := &cField{}

				for i := 0; i < current.Len(); i++ {
					i64 = int64(i)

					v.misc = append(v.misc[0:0], cf.name...)
					v.misc = append(v.misc, '[')
					v.misc = strconv.AppendInt(v.misc, i64, 10)
					v.misc = append(v.misc, ']')

					reusableCF.name = string(v.misc)

					if cf.namesEqual {
						reusableCF.altName = reusableCF.name
					} else {

						v.misc = append(v.misc[0:0], cf.altName...)
						v.misc = append(v.misc, '[')
						v.misc = strconv.AppendInt(v.misc, i64, 10)
						v.misc = append(v.misc, ']')

						reusableCF.altName = string(v.misc)
					}
					err = v.traverseField(ctx, parent, current.Index(i), ns, structNs, reusableCF, ct)
					if err != nil {
						return
					}
				}

			case reflect.Map:

				var pv string
				reusableCF := &cField{}

				for _, key := range current.MapKeys() {

					pv = fmt.Sprintf("%v", key.Interface())

					v.misc = append(v.misc[0:0], cf.name...)
					v.misc = append(v.misc, '[')
					v.misc = append(v.misc, pv...)
					v.misc = append(v.misc, ']')

					reusableCF.name = string(v.misc)

					if cf.namesEqual {
						reusableCF.altName = reusableCF.name
					} else {
						v.misc = append(v.misc[0:0], cf.altName...)
						v.misc = append(v.misc, '[')
						v.misc = append(v.misc, pv...)
						v.misc = append(v.misc, ']')

						reusableCF.altName = string(v.misc)
					}

					if ct != nil && ct.typeof == typeKeys && ct.keys != nil {
						err = v.traverseField(ctx, parent, key, ns, structNs, reusableCF, ct.keys)
						// can be nil when just keys being validated
						if ct.next != nil {
							err = v.traverseField(ctx, parent, current.MapIndex(key), ns, structNs, reusableCF, ct.next)
						}
					} else {
						err = v.traverseField(ctx, parent, current.MapIndex(key), ns, structNs, reusableCF, ct)
					}

					if err != nil {
						return
					}
				}

			default:
				// throw error, if not a slice or map then should not have gotten here
				// bad dive tag
				panic("dive error! can't dive on a non slice or map")
			}
			return

		case typeOr:
			v.misc = v.misc[0:0]
			for {

				// set Field Level fields
				v.slflParent = parent
				v.flField = current
				v.cf = cf
				v.ct = ct

				if ct.fn(ctx, v) {
					if ct.isBlockEnd {
						ct = ct.next
						continue OUTER
					}

					// drain rest of the 'or' values, then continue or leave
					for {

						ct = ct.next

						if ct == nil {
							return
						}

						if ct.typeof != typeOr {
							continue OUTER
						}

						if ct.isBlockEnd {
							ct = ct.next
							continue OUTER
						}
					}
				}

				v.misc = append(v.misc, '|')
				v.misc = append(v.misc, ct.tag...)

				if ct.hasParam {
					v.misc = append(v.misc, '=')
					v.misc = append(v.misc, ct.param...)
				}

				if ct.isBlockEnd || ct.next == nil {
					// if we get here, no valid 'or' value and no more tags
					v.str1 = string(append(ns, cf.altName...))
					v.str2 = v.str1

					if ct.hasAlias {
						err = fmt.Errorf(errMsg, ct.aliasTag)
					} else {
						tVal := string(v.misc)[1:]
						err = fmt.Errorf(errMsg, tVal)
					}
					return
				}

				ct = ct.next
			}

		default:
			// set Field Level fields
			v.slflParent = parent
			v.flField = current
			v.cf = cf
			v.ct = ct

			if !ct.fn(ctx, v) {
				v.str1 = string(append(ns, cf.altName...))
				v.str2 = v.str1
				err = fmt.Errorf(errMsg, ct.aliasTag)
				return
			}
			ct = ct.next
		}
	}
}
