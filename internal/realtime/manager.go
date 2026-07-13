package realtime

import (
	"fmt"
	"sync"

	"github.com/sgtLongs/go-website/internal/persistence"
)

type Manager struct {
	mu      sync.Mutex
	rooms   map[string]*Room
	onEmpty func(string)
	store   *persistence.Store
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

func NewPersistentManager(store *persistence.Store, onEmpty func(string)) (*Manager, error) {
	m := &Manager{rooms: make(map[string]*Room), onEmpty: onEmpty, store: store}
	if err := store.ForEach(persistence.RoomsBucket, func(key, value []byte) error {
		room, err := restoreRoom(value, m.closeRoom, store)
		if err != nil {
			return fmt.Errorf("restore room %q: %w", string(key), err)
		}
		if room.id != string(key) {
			return fmt.Errorf("restore room %q: storage key does not match room ID", string(key))
		}
		m.rooms[room.id] = room
		go room.run()
		return nil
	}); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Room(id string) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()

	if room, ok := m.rooms[id]; ok {
		return room
	}

	room := newRoomWithStore(id, m.closeRoom, m.store)
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
