package serv

import (
	"testing"
)

func TestRailsEncryptedSession(t *testing.T) {
	cookie := "dDdjMW5jYUNYaFpBT1BSdFgwQkk4ZWNlT214L1FnM0pyZzZ1d21nSnVTTm9zS0ljN000S1JmT3cxcTNtRld2Ny0tQUFBQUFBQUFBQUFBQUFBQUFBQUFBQT09--75d8323b0f0e41cf4d5aabee1b229b1be76b83b6"

	secret := "development_secret"

	userID, err := railsAuth(cookie, secret)
	if err != nil {
		t.Error(err)
		return
	}

	if userID != "1" {
		t.Errorf("Expecting userID 1 got %s", userID)
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
