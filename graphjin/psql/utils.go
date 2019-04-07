package psql

import "regexp"

func NewVariables(varlist map[string]string) map[string]string {
	re := regexp.MustCompile(`(?mi)\$([a-zA-Z0-9_.]+)`)
	vars := make(map[string]string, len(varlist))

	for k, v := range varlist {
		vars[k] = re.ReplaceAllString(v, `{{$1}}`)
	}
	return vars
}
