package qcode

func GetQType(gql string) QType {
	for i := range gql {
		b := gql[i]
		if b == '{' {
			return QTQuery
		}
		if al(b) {
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
