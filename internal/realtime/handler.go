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
	resolve  func(*gin.Context, string) (string, string, bool, bool)
}

func NewHandler(service *Service, resolve ...func(*gin.Context, string) (string, string, bool, bool)) *Handler {
	var resolveParticipant func(*gin.Context, string) (string, string, bool, bool)
	if len(resolve) > 0 {
		resolveParticipant = resolve[0]
	}
	return &Handler{
		service: service,
		resolve: resolveParticipant,
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
	participant := Participant{ID: name, Name: name}
	if h.resolve != nil {
		id, canonicalName, host, ok := h.resolve(c, name)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "join this lobby before connecting"})
			return
		}
		participant = Participant{ID: id, Name: canonicalName, Host: host}
	}

	responseHeader := http.Header{}
	for _, protocol := range websocket.Subprotocols(c.Request) {
		if strings.HasPrefix(protocol, "lobby-tab-token.") {
			responseHeader.Set("Sec-WebSocket-Protocol", protocol)
			break
		}
	}
	connection, err := h.upgrader.Upgrade(c.Writer, c.Request, responseHeader)
	if err != nil {
		return
	}

	// HandleConnection blocks until this browser disconnects. Gin gives each
	// request its own goroutine, so other requests continue normally.
	h.service.HandleConnection(roomID, participant, connection)
}

func sameHostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}
