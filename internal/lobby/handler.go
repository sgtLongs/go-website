package lobby

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

const accessCookie = "lobby_access"

type Handler struct {
	service     *Service
	playerCount func(string) int
}

func NewHandler(service *Service, playerCount func(string) int) *Handler {
	return &Handler{service: service, playerCount: playerCount}
}

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (h *Handler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"lobbies": h.service.List(h.playerCount)})
}

func (h *Handler) Create(c *gin.Context) {
	var input credentials
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and password are required"})
		return
	}
	l, token, err := h.service.Create(input.Name, input.Password)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrInvalidName) || errors.Is(err, ErrInvalidPassword) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	h.setAccessCookie(c, token)
	c.JSON(http.StatusCreated, gin.H{"id": l.ID, "name": l.Name})
}

func (h *Handler) Join(c *gin.Context) {
	var input credentials
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password is required"})
		return
	}
	token, err := h.service.Join(c.Param("lobbyID"), input.Password)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	h.setAccessCookie(c, token)
	c.Status(http.StatusNoContent)
}

func (h *Handler) AuthorizedRequest(c *gin.Context) bool {
	token, err := c.Cookie(accessCookie)
	return err == nil && h.service.Authorized(c.Param("roomID"), token)
}

func (h *Handler) HostRequest(c *gin.Context) bool {
	token, err := c.Cookie(accessCookie)
	return err == nil && h.service.IsHost(c.Param("roomID"), token)
}

func (h *Handler) RequireRoomAccess(c *gin.Context) {
	if !h.AuthorizedRequest(c) {
		c.Redirect(http.StatusSeeOther, "/")
		c.Abort()
		return
	}
	c.Next()
}

func (h *Handler) setAccessCookie(c *gin.Context, token string) {
	secure := c.Request.TLS != nil
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(accessCookie, token, int(accessLifetime.Seconds()), "/", "", secure, true)
}
