package realtime

import "sync"

type Manager struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

func (m *Manager) ParticipantCount(id string) int {
	m.mu.Lock()
	room := m.rooms[id]
	m.mu.Unlock()
	if room == nil {
		return 0
	}
	return int(room.count.Load())
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
