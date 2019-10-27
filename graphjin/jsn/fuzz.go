// +build gofuzz

package jsn

func Fuzz(data []byte) int {
	if err := unifiedTest(data); err != nil {
		return 0
	}

	return 1
}
