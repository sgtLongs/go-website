package realtime

import (
	"net/http"
	"net/url"
	"testing"
)

func TestValidRoomID(t *testing.T) {
	tests := map[string]bool{
		"lobby":       true,
		"family_room": true,
		"room-42":     true,
		"":            false,
		"has spaces":  false,
		"../secret":   false,
	}
	for roomID, want := range tests {
		if got := ValidRoomID(roomID); got != want {
			t.Errorf("ValidRoomID(%q) = %v, want %v", roomID, got, want)
		}
	}
}

func TestSameHostOrigin(t *testing.T) {
	request := &http.Request{
		Host:   "192.168.1.20:8080",
		Header: make(http.Header),
		URL:    &url.URL{Scheme: "http", Host: "192.168.1.20:8080", Path: "/ws"},
	}
	request.Header.Set("Origin", "http://192.168.1.20:8080")
	if !sameHostOrigin(request) {
		t.Fatal("expected matching LAN origin to be accepted")
	}

	request.Header.Set("Origin", "http://attacker.example")
	if sameHostOrigin(request) {
		t.Fatal("expected a different origin to be rejected")
	}
}
