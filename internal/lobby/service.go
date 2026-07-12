package lobby

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
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
	lobbyID   string
	expiresAt time.Time
	host      bool
}

// Service owns lobby metadata and short-lived lobby access grants. It is safe
// for concurrent HTTP requests.
type Service struct {
	mu      sync.RWMutex
	lobbies map[string]Lobby
	grants  map[string]accessGrant
	now     func() time.Time
}

func NewService() *Service {
	return &Service{
		lobbies: make(map[string]Lobby),
		grants:  make(map[string]accessGrant),
		now:     time.Now,
	}
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
	s.lobbies[l.ID] = l
	s.grants[token] = accessGrant{lobbyID: l.ID, expiresAt: s.now().Add(accessLifetime), host: true}
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
	s.grants[token] = accessGrant{lobbyID: id, expiresAt: s.now().Add(accessLifetime)}
	s.mu.Unlock()
	return token, nil
}

func (s *Service) Authorized(id, token string) bool {
	s.mu.RLock()
	grant, ok := s.grants[token]
	_, lobbyExists := s.lobbies[id]
	s.mu.RUnlock()
	return lobbyExists && ok && grant.lobbyID == id && s.now().Before(grant.expiresAt)
}

func (s *Service) IsHost(id, token string) bool {
	s.mu.RLock()
	grant, ok := s.grants[token]
	s.mu.RUnlock()
	return ok && grant.host && grant.lobbyID == id && s.now().Before(grant.expiresAt)
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
