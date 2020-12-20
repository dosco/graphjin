package qcode

func GetQType(gql string) (QType, string) {
	var tok string
	s := -1

	var skip byte
	sc := 0
	bc := 0

	for i := range gql {
		b := gql[i]
		switch {
		case b == '#':
			skip = '\n'
			sc++

		case sc == 0 && (b == '\'' || b == '"'):
			skip = b
			sc++

		case sc != 0 && i != 0 && gql[i-1] != '\\' && (b == skip):
			sc--

		case sc != 0:
			continue

		case b == '{':
			switch tok {
			case "", "query":
				return QTQuery, ""
			case "mutation":
				return QTMutation, ""
			case "subscription":
				return QTSubscription, ""
			}
			bc++

		case b == '}':
			bc++

		case s == -1 && al(b):
			s = i

		case s != -1 && !al(b):
			ct := gql[s:i]
			if (bc % 2) == 0 {
				switch tok {
				case "query":
					return QTQuery, ct
				case "mutation":
					return QTMutation, ct
				case "subscription":
					return QTSubscription, ct
				}
			}
			tok = ct
			s = -1
		}
	}
	return QTUnknown, ""
}

func al(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
