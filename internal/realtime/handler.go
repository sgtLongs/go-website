package realtime

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Handler translates Gin requests into calls to the realtime service.
type Handler struct {
	service  *Service
	upgrader websocket.Upgrader
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     sameHostOrigin,
		},
	}
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

	// HandleConnection blocks until this browser disconnects. Gin gives each
	// request its own goroutine, so other requests continue normally.
	h.service.HandleConnection(roomID, name, connection)
}

func sameHostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}
