package realtime

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var roomIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Handler struct {
	manager  *Manager
	upgrader websocket.Upgrader
}

func NewHandler(manager *Manager) *Handler {
	return &Handler{
		manager: manager,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     sameHostOrigin,
		},
	}
}

func ValidRoomID(roomID string) bool {
	return roomIDPattern.MatchString(roomID)
}

func (h *Handler) ServeWebSocket(c *gin.Context) {
	roomID := c.Param("roomID")
	name := strings.TrimSpace(c.Query("name"))
	if !ValidRoomID(roomID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room ID"})
		return
	}
	if name == "" || len([]rune(name)) > 40 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1 to 40 characters"})
		return
	}

	connection, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	client := &Client{
		participant: Participant{ID: uuid.NewString(), Name: name},
		room:        h.manager.Room(roomID),
		connection:  connection,
		send:        make(chan []byte, 32),
	}
	client.room.register <- client

	go client.writePump()
	client.readPump()
}

func sameHostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}
