package realtime

import "sync"

// Manager owns the collection of active rooms. The mutex protects the map
// because WebSocket requests can arrive concurrently.
type Manager struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

func NewManager() *Manager {
	return &Manager{rooms: make(map[string]*Room)}
}

func (m *Manager) Room(id string) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()

	if room, ok := m.rooms[id]; ok {
		return room
	}

	room := newRoom(id)
	m.rooms[id] = room
	go room.run()
	return room
}
