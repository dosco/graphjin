//go:build gofuzz
// +build gofuzz

package serv

func Fuzz(data []byte) int {
	gql := string(data)
	graph.FastParse(gql)
	gqlHash(gql, nil, "")

	return 1
}
