package psql

import "fmt"

func (rt RelType) String() string {
	switch rt {
	case RelOneToOne:
		return "one to one"
	case RelOneToMany:
		return "one to many"
	case RelOneToManyThrough:
		return "one to many through"
	case RelRemote:
		return "remote"
	}
	return ""
}

func (re *DBRel) String() string {
	return fmt.Sprintf("'%s.%s' --(%s)--> '%s.%s'",
		re.Left.Table, re.Left.Col, re.Type, re.Right.Table, re.Right.Col)
}
