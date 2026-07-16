package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sgtLongs/go-website/frontend"
	"github.com/sgtLongs/go-website/internal/lobby"
	"github.com/sgtLongs/go-website/internal/persistence"
	"github.com/sgtLongs/go-website/internal/realtime"
)

func main() {
	store, err := persistence.Open(envOrDefault("DATA_PATH", "data/game.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close persistence database: %v", err)
		}
	}()
	basePath, err := normalizeBasePath(os.Getenv("BASE_PATH"))
	if err != nil {
		log.Fatalf("invalid BASE_PATH: %v", err)
	}
	var router *gin.Engine = newRouterWithBasePath(basePath, store)
	address := envOrDefault("ADDRESS", ":8080")

	server := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server listening on %s", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newRouter(stores ...*persistence.Store) *gin.Engine {
	return newRouterWithBasePath("", stores...)
}

func newRouterWithBasePath(basePath string, stores ...*persistence.Store) *gin.Engine {
	var err error
	basePath, err = normalizeBasePath(basePath)
	if err != nil {
		panic(err)
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	var lobbyService *lobby.Service
	var presenceService *realtime.Service
	if len(stores) == 0 {
		lobbyService = lobby.NewService()
		presenceService = realtime.NewService(lobbyService.Close)
	} else {
		var err error
		lobbyService, err = lobby.NewPersistentService(stores[0])
		if err != nil {
			panic(err)
		}
		presenceService, err = realtime.NewPersistentService(stores[0], lobbyService.Close)
		if err != nil {
			panic(err)
		}
	}
	lobbyHandler := lobby.NewHandlerWithBasePath(lobbyService, presenceService.ParticipantCount, basePath)
	realtimeHandler := realtime.NewHandler(presenceService, lobbyHandler.ResolveRoomParticipant)
	assets, err := fs.Sub(frontend.Files, "assets")
	if err != nil {
		panic(err)
	}
	pages, err := template.ParseFS(frontend.Files, "html/*.html")
	if err != nil {
		panic(err)
	}
	routes := router.Group(basePath)
	baseHref := basePath + "/"
	renderPage := func(c *gin.Context, name string) {
		var contents bytes.Buffer
		if err := pages.ExecuteTemplate(&contents, name, struct{ BaseHref string }{BaseHref: baseHref}); err != nil {
			c.String(http.StatusInternalServerError, "could not load page")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", contents.Bytes())
	}

	if basePath != "" {
		router.GET(basePath, func(c *gin.Context) {
			c.Redirect(http.StatusPermanentRedirect, baseHref)
		})
	}
	routes.GET("/", func(c *gin.Context) {
		renderPage(c, "index.html")
	})
	routes.StaticFS("/assets", http.FS(assets))
	routes.GET("/room/:roomID", lobbyHandler.RequireRoomAccess, func(c *gin.Context) {
		if !realtime.ValidRoomID(c.Param("roomID")) {
			c.String(http.StatusBadRequest, "invalid room ID")
			return
		}
		renderPage(c, "room.html")
	})
	routes.GET("/api/lobbies", lobbyHandler.List)
	routes.POST("/api/lobbies", lobbyHandler.Create)
	routes.POST("/api/lobbies/:lobbyID/join", lobbyHandler.Join)
	routes.POST("/api/lobbies/:lobbyID/tab-session", lobbyHandler.CreateTabSession)
	routes.GET("/ws/rooms/:roomID", realtimeHandler.ServeWebSocket)
	routes.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return router
}

func normalizeBasePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return "", nil
	}
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("must start with /")
	}

	value = strings.TrimRight(value, "/")
	if cleaned := path.Clean(value); cleaned != value {
		return "", fmt.Errorf("must not contain empty, . or .. path segments")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if segment == "" {
			return "", fmt.Errorf("must not contain empty path segments")
		}
		for _, character := range segment {
			if !validBasePathCharacter(character) {
				return "", fmt.Errorf("contains unsupported character %q", character)
			}
		}
	}
	return value, nil
}

func validBasePathCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~", character)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
