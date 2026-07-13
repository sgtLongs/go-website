// Package realtime implements HTTP and application behavior for realtime presence.
package realtime

import (
	"regexp"

	"github.com/gorilla/websocket"
	"github.com/sgtLongs/go-website/internal/persistence"
)

var roomIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Service is the controller-facing entry point to presence behavior.
type Service struct {
	manager *Manager
}

func NewService(onEmpty func(string)) *Service {
	return &Service{manager: NewManager(onEmpty)}
}

func NewPersistentService(store *persistence.Store, onEmpty func(string)) (*Service, error) {
	manager, err := NewPersistentManager(store, onEmpty)
	if err != nil {
		return nil, err
	}
	return &Service{manager: manager}, nil
}

func ValidRoomID(roomID string) bool {
	return roomIDPattern.MatchString(roomID)
}

func (s *Service) ParticipantCount(roomID string) int {
	return s.manager.ParticipantCount(roomID)
}

// HandleConnection registers one browser connection and owns it until the
// connection closes. HTTP-specific validation stays in the controller.
func (s *Service) HandleConnection(roomID string, participant Participant, connection *websocket.Conn) {
	room := s.manager.Room(roomID)
	client := &Client{
		participant: participant,
		room:        room,
		connection:  connection,
		send:        make(chan []byte, 32),
	}
	room.register <- client

	go client.writePump()
	client.readPump()
}
