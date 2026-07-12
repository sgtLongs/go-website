package realtime

import "encoding/json"

type Room struct {
	id         string
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

func newRoom(id string) *Room {
	return &Room{
		id:         id,
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 32),
	}
}

func (r *Room) run() {
	for {
		select {
		case client := <-r.register:
			r.clients[client] = struct{}{}
			r.sendSnapshot(client)
			r.broadcastEvent("user_joined", client.participant)

		case client := <-r.unregister:
			if _, exists := r.clients[client]; !exists {
				continue
			}
			delete(r.clients, client)
			close(client.send)
			r.broadcastEvent("user_left", client.participant)

		case message := <-r.broadcast:
			for client := range r.clients {
				if !r.queue(client, message) {
					delete(r.clients, client)
					close(client.send)
				}
			}
		}
	}
}

func (r *Room) sendSnapshot(client *Client) {
	participants := make([]Participant, 0, len(r.clients))
	for connected := range r.clients {
		participants = append(participants, connected.participant)
	}
	r.queueEvent(client, Event{
		Type:   "presence_snapshot",
		RoomID: r.id,
		Data:   presenceSnapshot{Participants: participants},
	})
}

func (r *Room) broadcastEvent(eventType string, data any) {
	message, err := json.Marshal(Event{Type: eventType, RoomID: r.id, Data: data})
	if err != nil {
		return
	}
	for client := range r.clients {
		r.queue(client, message)
	}
}

func (r *Room) queueEvent(client *Client, event Event) {
	message, err := json.Marshal(event)
	if err == nil {
		r.queue(client, message)
	}
}

func (r *Room) queue(client *Client, message []byte) bool {
	select {
	case client.send <- message:
		return true
	default:
		return false
	}
}
