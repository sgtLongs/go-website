package lobby

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sgtLongs/go-website/internal/persistence"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidName     = errors.New("lobby name must be 1 to 60 characters")
	ErrInvalidPassword = errors.New("password must be 4 to 72 characters")
	ErrNotFound        = errors.New("lobby not found")
	ErrWrongPassword   = errors.New("incorrect password")
)

const accessLifetime = 12 * time.Hour

type Lobby struct {
	ID           string
	Name         string
	passwordHash []byte
	CreatedAt    time.Time
}

type Summary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	PlayerCount int       `json:"playerCount"`
}

type accessGrant struct {
	lobbyID       string
	participantID string
	displayName   string
	expiresAt     time.Time
	host          bool
}

type persistedLobby struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PasswordHash []byte    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
}

type persistedGrant struct {
	LobbyID       string    `json:"lobbyId"`
	ParticipantID string    `json:"participantId"`
	DisplayName   string    `json:"displayName,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Host          bool      `json:"host"`
}

// Service owns lobby metadata and short-lived lobby access grants. It is safe
// for concurrent HTTP requests.
type Service struct {
	mu      sync.RWMutex
	lobbies map[string]Lobby
	grants  map[string]accessGrant
	now     func() time.Time
	store   *persistence.Store
}

func NewService() *Service {
	return &Service{
		lobbies: make(map[string]Lobby),
		grants:  make(map[string]accessGrant),
		now:     time.Now,
	}
}

func NewPersistentService(store *persistence.Store) (*Service, error) {
	s := NewService()
	s.store = store
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Create(name, password string) (Lobby, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return Lobby{}, "", ErrInvalidName
	}
	if len(password) < 4 || len(password) > 72 {
		return Lobby{}, "", ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Lobby{}, "", err
	}

	l := Lobby{ID: uuid.NewString(), Name: name, passwordHash: hash, CreatedAt: s.now().UTC()}
	token, err := newToken()
	if err != nil {
		return Lobby{}, "", err
	}
	s.mu.Lock()
	grant := accessGrant{lobbyID: l.ID, participantID: uuid.NewString(), expiresAt: s.now().Add(accessLifetime), host: true}
	if err := s.persistLobbyAndGrant(l, token, grant); err != nil {
		s.mu.Unlock()
		return Lobby{}, "", err
	}
	s.lobbies[l.ID] = l
	s.grants[tokenKey(token)] = grant
	s.mu.Unlock()
	return l, token, nil
}

func (s *Service) List(playerCount func(string) int) []Summary {
	s.mu.RLock()
	result := make([]Summary, 0, len(s.lobbies))
	for _, l := range s.lobbies {
		result = append(result, Summary{ID: l.ID, Name: l.Name, CreatedAt: l.CreatedAt, PlayerCount: playerCount(l.ID)})
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (s *Service) Join(id, password string) (string, error) {
	s.mu.RLock()
	l, ok := s.lobbies[id]
	s.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	if bcrypt.CompareHashAndPassword(l.passwordHash, []byte(password)) != nil {
		return "", ErrWrongPassword
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	grant := accessGrant{lobbyID: id, participantID: uuid.NewString(), expiresAt: s.now().Add(accessLifetime)}
	if err := s.persistGrant(token, grant); err != nil {
		s.mu.Unlock()
		return "", err
	}
	s.grants[tokenKey(token)] = grant
	s.mu.Unlock()
	return token, nil
}

// NewTabGrant creates an independent participant identity for a browser tab
// while preserving the access level of the browser-wide grant.
func (s *Service) NewTabGrant(id, accessToken string) (string, error) {
	s.mu.RLock()
	parent, ok := s.grants[tokenKey(accessToken)]
	_, lobbyExists := s.lobbies[id]
	s.mu.RUnlock()
	if !lobbyExists || !ok || parent.lobbyID != id || !s.now().Before(parent.expiresAt) {
		return "", ErrNotFound
	}

	token, err := newToken()
	if err != nil {
		return "", err
	}
	grant := accessGrant{
		lobbyID:       id,
		participantID: uuid.NewString(),
		expiresAt:     parent.expiresAt,
		host:          parent.host,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistGrant(token, grant); err != nil {
		return "", err
	}
	s.grants[tokenKey(token)] = grant
	return token, nil
}

func (s *Service) Authorized(id, token string) bool {
	s.mu.RLock()
	grant, ok := s.grants[tokenKey(token)]
	_, lobbyExists := s.lobbies[id]
	s.mu.RUnlock()
	return lobbyExists && ok && grant.lobbyID == id && s.now().Before(grant.expiresAt)
}

func (s *Service) IsHost(id, token string) bool {
	s.mu.RLock()
	grant, ok := s.grants[tokenKey(token)]
	s.mu.RUnlock()
	return ok && grant.host && grant.lobbyID == id && s.now().Before(grant.expiresAt)
}

// TransferHost makes one participant the sole host for a lobby. Every grant
// for the previous host is demoted so an old browser session cannot restore
// host privileges after a realtime-room handoff.
func (s *Service) TransferHost(id, participantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.lobbies[id]; !exists {
		return ErrNotFound
	}

	updated := make(map[string]accessGrant)
	entries := make([]persistence.Entry, 0)
	found := false
	for key, grant := range s.grants {
		if grant.lobbyID != id {
			continue
		}
		grant.host = grant.participantID == participantID
		found = found || grant.host
		updated[key] = grant
		if s.store != nil {
			encoded, err := marshalGrant(grant)
			if err != nil {
				return err
			}
			entries = append(entries, persistence.Entry{Bucket: persistence.GrantsBucket, Key: []byte(key), Value: encoded})
		}
	}
	if !found {
		return ErrNotFound
	}
	if s.store != nil {
		if err := s.store.PutAll(entries...); err != nil {
			return err
		}
	}
	for key, grant := range updated {
		s.grants[key] = grant
	}
	return nil
}

// ResolveParticipant binds a display name to a lobby access grant once and
// returns the stable, server-owned identity on every later connection.
func (s *Service) ResolveParticipant(id, token, requestedName string) (participantID, displayName string, host, ok bool) {
	requestedName = strings.TrimSpace(requestedName)
	key := tokenKey(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, exists := s.grants[key]
	if !exists || grant.lobbyID != id || !s.now().Before(grant.expiresAt) {
		return "", "", false, false
	}
	if _, exists := s.lobbies[id]; !exists {
		return "", "", false, false
	}
	if grant.displayName == "" {
		if requestedName == "" || len([]rune(requestedName)) > 40 {
			return "", "", false, false
		}
		grant.displayName = requestedName
		if err := s.persistGrantByKey(key, grant); err != nil {
			log.Printf("persist participant identity: %v", err)
			return "", "", false, false
		}
		s.grants[key] = grant
	}
	return grant.participantID, grant.displayName, grant.host, true
}

// Close removes a lobby after its realtime room's empty grace period. Removing
// its grants prevents stale browser tabs from reopening the closed lobby.
func (s *Service) Close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	grantKeys := make([][]byte, 0)
	for key, grant := range s.grants {
		if grant.lobbyID == id {
			grantKeys = append(grantKeys, []byte(key))
		}
	}
	if s.store != nil {
		if err := s.store.DeleteLobby(id, grantKeys); err != nil {
			log.Printf("delete closed lobby %s: %v", id, err)
			return
		}
	}
	delete(s.lobbies, id)
	for token, grant := range s.grants {
		if grant.lobbyID == id {
			delete(s.grants, token)
		}
	}
}

func (s *Service) load() error {
	if err := s.store.ForEach(persistence.LobbiesBucket, func(_ []byte, value []byte) error {
		var stored persistedLobby
		if err := json.Unmarshal(value, &stored); err != nil {
			return err
		}
		if stored.ID == "" || stored.Name == "" || len(stored.PasswordHash) == 0 {
			return errors.New("invalid persisted lobby")
		}
		s.lobbies[stored.ID] = Lobby{ID: stored.ID, Name: stored.Name, passwordHash: stored.PasswordHash, CreatedAt: stored.CreatedAt}
		return nil
	}); err != nil {
		return fmt.Errorf("load lobbies: %w", err)
	}
	var expired [][]byte
	if err := s.store.ForEach(persistence.GrantsBucket, func(key, value []byte) error {
		var stored persistedGrant
		if err := json.Unmarshal(value, &stored); err != nil {
			return err
		}
		if !s.now().Before(stored.ExpiresAt) {
			expired = append(expired, key)
			return nil
		}
		if stored.LobbyID == "" || stored.ParticipantID == "" {
			return errors.New("invalid persisted access grant")
		}
		s.grants[string(key)] = accessGrant{
			lobbyID: stored.LobbyID, participantID: stored.ParticipantID, displayName: stored.DisplayName,
			expiresAt: stored.ExpiresAt, host: stored.Host,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load access grants: %w", err)
	}
	for _, key := range expired {
		if err := s.store.Delete(persistence.GrantsBucket, key); err != nil {
			return fmt.Errorf("delete expired access grant: %w", err)
		}
	}
	return nil
}

func (s *Service) persistLobbyAndGrant(l Lobby, token string, grant accessGrant) error {
	if s.store == nil {
		return nil
	}
	lobbyJSON, err := json.Marshal(persistedLobby{ID: l.ID, Name: l.Name, PasswordHash: l.passwordHash, CreatedAt: l.CreatedAt})
	if err != nil {
		return err
	}
	grantJSON, err := marshalGrant(grant)
	if err != nil {
		return err
	}
	return s.store.PutAll(
		persistence.Entry{Bucket: persistence.LobbiesBucket, Key: []byte(l.ID), Value: lobbyJSON},
		persistence.Entry{Bucket: persistence.GrantsBucket, Key: []byte(tokenKey(token)), Value: grantJSON},
	)
}

func (s *Service) persistGrant(token string, grant accessGrant) error {
	return s.persistGrantByKey(tokenKey(token), grant)
}

func (s *Service) persistGrantByKey(key string, grant accessGrant) error {
	if s.store == nil {
		return nil
	}
	encoded, err := marshalGrant(grant)
	if err != nil {
		return err
	}
	return s.store.Put(persistence.GrantsBucket, []byte(key), encoded)
}

func marshalGrant(grant accessGrant) ([]byte, error) {
	return json.Marshal(persistedGrant{
		LobbyID: grant.lobbyID, ParticipantID: grant.participantID, DisplayName: grant.displayName,
		ExpiresAt: grant.expiresAt, Host: grant.host,
	})
}

func tokenKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return string(digest[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
