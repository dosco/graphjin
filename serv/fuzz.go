// +build gofuzz

package serv

func Fuzz(data []byte) int {
	gql := string(data)
	QueryName(gql)
	gqlHash(gql, nil, "")

	return 1
}
