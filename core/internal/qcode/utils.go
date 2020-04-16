package qcode

func GetQType(gql string) QType {
	ic := false
	for i := range gql {
		b := gql[i]
		switch {
		case b == '#':
			ic = true
		case b == '\n':
			ic = false
		case !ic && b == '{':
			return QTQuery
		case !ic && al(b):
			switch b {
			case 'm', 'M':
				return QTMutation
			case 'q', 'Q':
				return QTQuery
			}
		}
	}
	return -1
}

func al(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func (qt QType) String() string {
	switch qt {
	case QTQuery:
		return "query"
	case QTMutation:
		return "mutation"
	case QTInsert:
		return "insert"
	case QTUpdate:
		return "update"
	case QTDelete:
		return "delete"
	case QTUpsert:
		return "upsert"
	}

	return ""
}
