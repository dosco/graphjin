package auth_test

import (
	"net/http"
	"testing"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/stretchr/testify/assert"
)

func TestJWTTokenInAuthorizationHeader(t *testing.T) {
	ah, err := auth.JwtHandler(auth.Auth{
		Cookie: "Boo",
		JWT: auth.JWTConfig{
			Secret: "casper",
		},
	})
	assert.NoError(t, err)

	// {
	// 	"sub": "1234567890",
	// 	"name": "John Doe",
	// 	"iat": 1516239022
	//   }
	tok := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.VZ01qXI7Whbuj8X3FZw0mLyZT7iMKMCDl_rtzdNpjAg"

	req, err := http.NewRequest(
		http.MethodGet,
		"https://test.com",
		nil)
	assert.NoError(t, err)

	req.Header.Set("Authorization", "Bearer "+tok)

	c, err := ah(nil, req)
	assert.NoError(t, err)

	assert.Equal(t, 1234567890, auth.UserIDInt(c))
}
