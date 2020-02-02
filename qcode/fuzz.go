// +build gofuzz

package qcode

// FuzzerEntrypoint for Fuzzbuzz
func Fuzz(data []byte) int {
	qt := GetQType(string(data))

	if qt > QTUpsert {
		panic("qt > QTUpsert")
	}

	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.Compile(data, "user")
	if err != nil {
		return 0
	}

	return 1
}
