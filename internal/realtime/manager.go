package realtime

import "sync"

type Manager struct {
	mu      sync.Mutex
	rooms   map[string]*Room
	onEmpty func(string)
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

func NewManager(onEmpty func(string)) *Manager {
	return &Manager{rooms: make(map[string]*Room), onEmpty: onEmpty}
}

func (m *Manager) Room(id string) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()

	if room, ok := m.rooms[id]; ok {
		return room
	}

	room := newRoom(id, m.closeRoom)
	m.rooms[id] = room
	go room.run()
	return room
}

func (m *Manager) closeRoom(room *Room) {
	m.mu.Lock()
	if m.rooms[room.id] != room {
		m.mu.Unlock()
		return
	}
	delete(m.rooms, room.id)
	onEmpty := m.onEmpty
	m.mu.Unlock()
	if onEmpty != nil {
		onEmpty(room.id)
	}
}
