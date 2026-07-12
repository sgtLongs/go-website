package lobby

import "testing"

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

func TestCreateValidation(t *testing.T) {
	s := NewService()
	if _, _, err := s.Create("", "secret"); err != ErrInvalidName {
		t.Fatalf("error = %v", err)
	}
	if _, _, err := s.Create("name", "abc"); err != ErrInvalidPassword {
		t.Fatalf("error = %v", err)
	}
}
