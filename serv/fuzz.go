// +build gofuzz

package serv

func Fuzz(data []byte) int {
	gql := string(data)
	gqlHash(gql, nil, "")

	return 1
}
