//go:build gofuzz
// +build gofuzz

package serv

func Fuzz(data []byte) int {
	gql := string(data)
	graph.FastParse(gql)

	return 1
}
