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
	case RelEmbedded:
		return "embedded"
	case RelPolymorphic:
		return "polymorphic"
	}
	return ""
}

func (re *DBRel) String() string {
	if re.Type == RelOneToManyThrough {
		return fmt.Sprintf("'%s.%s' --(%s.%s, %s.%s)--> '%s.%s'",
			re.Left.Table, re.Left.Col,
			re.Through.Table, re.Through.ColL, re.Through.Table, re.Through.ColR,
			re.Right.Table, re.Right.Col)
	}
	return fmt.Sprintf("'%s.%s' --(%s)--> '%s.%s'",
		re.Left.Table, re.Left.Col, re.Type, re.Right.Table, re.Right.Col)
}
