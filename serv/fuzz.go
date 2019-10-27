// +build gofuzz

package serv

func Fuzz(data []byte) int {
	gql := string(data)
	isMutation(gql)
	gqlHash(gql, nil, "")

	return 1
}
