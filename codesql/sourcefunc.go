package codesql

import (
	"database/sql/driver"
	"fmt"
	"os"

	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterScalarFunction("codesql_source", 4, codeSQLSourceFunc)
}

func codeSQLSourceFunc(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) != 4 {
		return "", fmt.Errorf("codesql_source expects 4 arguments")
	}
	path, _ := args[0].(string)
	if path == "" {
		return "", nil
	}
	start, ok := driverInt64(args[1])
	if !ok {
		return "", fmt.Errorf("codesql_source start byte must be numeric")
	}
	end, ok := driverInt64(args[2])
	if !ok {
		return "", fmt.Errorf("codesql_source end byte must be numeric")
	}
	withContext, _ := driverInt64(args[3])

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if start < 0 || end < start || end > int64(len(data)) {
		return "", fmt.Errorf("codesql_source byte range %d:%d outside %s", start, end, path)
	}
	if withContext != 0 {
		start, end = contextRange(data, start, end)
	}
	return string(data[start:end]), nil
}

func driverInt64(v driver.Value) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

func contextRange(data []byte, start, end int64) (int64, int64) {
	lineStart := start
	for lines := 0; lineStart > 0 && lines < 3; lineStart-- {
		if data[lineStart-1] == '\n' {
			lines++
		}
	}
	if lineStart < 0 {
		lineStart = 0
	}
	lineEnd := end
	for lines := 0; lineEnd < int64(len(data)) && lines < 3; lineEnd++ {
		if data[lineEnd] == '\n' {
			lines++
		}
	}
	return lineStart, lineEnd
}
