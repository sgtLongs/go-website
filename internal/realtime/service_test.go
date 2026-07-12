package realtime

import "testing"

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
