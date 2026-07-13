package lobby

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sgtLongs/go-website/internal/persistence"
)

func TestPersistentGrantRestoresStableIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lobbies.db")
	store, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewPersistentService(store)
	if err != nil {
		t.Fatal(err)
	}
	lobby, token, err := service.Create("Persistent room", "secret")
	if err != nil {
		t.Fatal(err)
	}
	playerID, name, host, ok := service.ResolveParticipant(lobby.ID, token, "Original name")
	if !ok || playerID == "" || name != "Original name" || !host {
		t.Fatalf("initial identity = %q, %q, %v, %v", playerID, name, host, ok)
	}
	if _, exists, err := store.Get(persistence.GrantsBucket, []byte(token)); err != nil || exists {
		t.Fatalf("raw bearer token persisted = %v, error = %v", exists, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := NewPersistentService(reopened)
	if err != nil {
		t.Fatal(err)
	}
	gotID, gotName, gotHost, gotOK := restored.ResolveParticipant(lobby.ID, token, "Tampered name")
	if !gotOK || gotID != playerID || gotName != name || gotHost != host {
		t.Fatalf("restored identity = %q, %q, %v, %v; want %q, %q, %v, true", gotID, gotName, gotHost, gotOK, playerID, name, host)
	}
}

func TestCreateJoinAndAuthorize(t *testing.T) {
	s := NewService()
	l, creatorToken, err := s.Create("Friday game", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Authorized(l.ID, creatorToken) {
		t.Fatal("creator should receive access")
	}
	if !s.IsHost(l.ID, creatorToken) {
		t.Fatal("creator should be the host")
	}
	if _, err := s.Join(l.ID, "wrong"); err != ErrWrongPassword {
		t.Fatalf("wrong password error = %v", err)
	}
	joinToken, err := s.Join(l.ID, "secret")
	if err != nil || !s.Authorized(l.ID, joinToken) {
		t.Fatalf("joining failed: %v", err)
	}
	if s.IsHost(l.ID, joinToken) {
		t.Fatal("a joining player must not become the host")
	}
	if s.Authorized("another-lobby", joinToken) {
		t.Fatal("grant must only authorize its own lobby")
	}
}

func TestListDoesNotExposePassword(t *testing.T) {
	s := NewService()
	l, _, err := s.Create("Visible name", "secret")
	if err != nil {
		t.Fatal(err)
	}
	items := s.List(func(id string) int {
		if id != l.ID {
			t.Fatalf("unexpected ID %q", id)
		}
		return 3
	})
	if len(items) != 1 || items[0].Name != "Visible name" || items[0].PlayerCount != 3 {
		t.Fatalf("unexpected list: %#v", items)
	}
}

func TestCloseRemovesLobbyAndAccessGrants(t *testing.T) {
	s := NewService()
	lobby, token, err := s.Create("Closing time", "secret")
	if err != nil {
		t.Fatal(err)
	}

	s.Close(lobby.ID)

	if s.Authorized(lobby.ID, token) {
		t.Fatal("closed lobby should not accept its previous access grant")
	}
	if _, err := s.Join(lobby.ID, "secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Join error = %v, want ErrNotFound", err)
	}
}

func TestCreateValidation(t *testing.T) {
	s := NewService()
	if _, _, err := s.Create("", "secret"); err != ErrInvalidName {
		t.Fatalf("error = %v", err)
	}
	if _, _, err := s.Create("name", "abc"); err != ErrInvalidPassword {
		t.Fatalf("error = %v", err)
	}
}
