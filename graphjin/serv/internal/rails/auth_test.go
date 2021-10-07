package rails

import (
	"testing"
)

func TestRailsEncryptedSession1(t *testing.T) {
	cookie := "dDdjMW5jYUNYaFpBT1BSdFgwQkk4ZWNlT214L1FnM0pyZzZ1d21nSnVTTm9zS0ljN000S1JmT3cxcTNtRld2Ny0tQUFBQUFBQUFBQUFBQUFBQUFBQUFBQT09--75d8323b0f0e41cf4d5aabee1b229b1be76b83b6"

	secret := "development_secret"

	ra := Auth{
		Cipher:   railsCipher,
		Secret:   secret,
		Salt:     salt,
		SignSalt: signSalt,
	}

	userID, err := ra.ParseCookie(cookie)
	if err != nil {
		t.Error(err)
		return
	}

	if userID != "1" {
		t.Errorf("Expecting userID 1 got %s", userID)
	}
}

func TestRailsEncryptedSession52(t *testing.T) {
	cookie :=
		"fZy1lt%2FIuXh2cpQgy3wWjbvabh1AqJX%2Bt6qO4D95DOZIpDhMyK2HqPFeNoaBtrXCUa9%2BDQuvbs1GX6tuccEAp14QPLNhm0PPJS5U1pRHqPLWaqT%2BBPYP%2BY9bo677komm9CPuOCOqBKf7rv3%2F4ptLmVO7iefB%2FP2ZlkV1848Johv5q%2B5PGyMxII2BEQnBdS3Petw6lRu741Bquc8z9VofC3t4%2F%2BLxVz%2BvBbTg--VL0MorYITXB8Dj3W--0yr0sr6pRU%2FwlYMQ%2BpEifA%3D%3D"

	// #nosec G101
	secret := "0a248500a64c01184edb4d7ad3a805488f8097ac761b76aaa6c17c01dcb7af03a2f18ba61b2868134b9c7b79a122bc0dadff4367414a2d173297bfea92be5566"

	ra := Auth{
		Cipher:   railsCipher52,
		Secret:   secret,
		AuthSalt: authSalt,
	}

	userID, err := ra.ParseCookie(cookie)
	if err != nil {
		t.Error(err)
		return
	}

	if userID != "2" {
		t.Errorf("Expecting userID 2 got %s", userID)
	}
}

func TestRailsJsonSession(t *testing.T) {
	sessionData := `{"warden.user.user.key":[[1],"secret"]}`

	userID, err := getUserId([]byte(sessionData))
	if err != nil {
		t.Error(err)
		return
	}

	if userID != "1" {
		t.Errorf("Expecting userID 1 got %s", userID)
	}
}

func TestRailsMarshaledSession(t *testing.T) {
	sessionData := "\x04\b{\bI\"\x15member_return_to\x06:\x06ETI\"\x06/\x06;\x00TI\"\x19warden.user.user.key\x06;\x00T[\a[\x06i\aI\"\"$2a$11$6SgXdvO9hld82kQAvpEY3e\x06;\x00TI\"\x10_csrf_token\x06;\x00FI\"17lqwj1UsTTgbXBQKH4ipCNW32uLusvfSPds1txppMec=\x06;\x00F"

	userID, err := getUserId4([]byte(sessionData))
	if err != nil {
		t.Error(err)
		return
	}

	if userID != "2" {
		t.Errorf("Expecting userID 2 got %s", userID)
	}
}
