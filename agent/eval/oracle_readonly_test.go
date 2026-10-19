package eval

import "testing"

func TestReadOnlyGraphQLAcceptsAnonymousQueryWithVariables(t *testing.T) {
	for query, want := range map[string]bool{
		"query { orders { count_id } }":                       true,
		"query($from: String!) { orders { count_id } }":       true,
		"query named($from: String!) { orders { count_id } }": true,
		"{ orders { count_id } }":                             true,
		"mutation { orders(delete: true) { id } }":            false,
		"subscription { orders { id } }":                      false,
	} {
		if got := readOnlyGraphQL(query); got != want {
			t.Errorf("readOnlyGraphQL(%q) = %v, want %v", query, got, want)
		}
	}
}
