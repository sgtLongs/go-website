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
	service   *Service
	upgrader  websocket.Upgrader
	authorize func(*gin.Context) bool
	isHost    func(*gin.Context) bool
}

func NewHandler(service *Service, authorize ...func(*gin.Context) bool) *Handler {
	var authorizeRequest func(*gin.Context) bool
	if len(authorize) > 0 {
		authorizeRequest = authorize[0]
	}
	var hostRequest func(*gin.Context) bool
	if len(authorize) > 1 {
		hostRequest = authorize[1]
	}
	return &Handler{
		service:   service,
		authorize: authorizeRequest,
		isHost:    hostRequest,
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
	if h.authorize != nil && !h.authorize(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "join this lobby before connecting"})
		return
	}

	connection, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	// HandleConnection blocks until this browser disconnects. Gin gives each
	// request its own goroutine, so other requests continue normally.
	host := h.isHost != nil && h.isHost(c)
	h.service.HandleConnection(roomID, name, host, connection)
}

func sameHostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}
