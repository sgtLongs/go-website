package realtime

import (
	"net/http"
	"net/url"
	"testing"
)

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
