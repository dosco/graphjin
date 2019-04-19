package qcode

// FuzzerEntrypoint for Fuzzbuzz
func FuzzerEntrypoint(data []byte) int {
	testData := string(data)

	qcompile, _ := NewCompiler(Config{})
	_, err := qcompile.CompileQuery(testData)
	if err != nil {
		return -1
	}

	return 0
}
