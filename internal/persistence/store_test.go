package persistence

import (
	"path/filepath"
	"testing"
)

func TestStorePersistsMultipleRoomsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rooms.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"room-one": `{"round":1}`, "room-two": `{"round":4}`} {
		if err := store.Put(RoomsBucket, []byte(key), []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for key, want := range map[string]string{"room-one": `{"round":1}`, "room-two": `{"round":4}`} {
		got, exists, err := reopened.Get(RoomsBucket, []byte(key))
		if err != nil || !exists || string(got) != want {
			t.Fatalf("room %q after reopen = %q, %v, %v; want %q, true, nil", key, got, exists, err, want)
		}
	}
}
