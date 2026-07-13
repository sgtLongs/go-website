package main

import (
	"io/fs"
	"log"
	"net/http"
	"os"
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
	var router *gin.Engine = newRouter(store)
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
	lobbyHandler := lobby.NewHandler(lobbyService, presenceService.ParticipantCount)
	realtimeHandler := realtime.NewHandler(presenceService, lobbyHandler.ResolveRoomParticipant)
	assets, err := fs.Sub(frontend.Files, "assets")
	if err != nil {
		panic(err)
	}

	router.GET("/", func(c *gin.Context) {
		contents, err := fs.ReadFile(frontend.Files, "html/index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "could not load index page")
			return
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", contents)
	})
	router.StaticFS("/assets", http.FS(assets))
	router.GET("/room/:roomID", lobbyHandler.RequireRoomAccess, func(c *gin.Context) {
		if !realtime.ValidRoomID(c.Param("roomID")) {
			c.String(http.StatusBadRequest, "invalid room ID")
			return
		}
		c.FileFromFS("html/room.html", http.FS(frontend.Files))
	})
	router.GET("/api/lobbies", lobbyHandler.List)
	router.POST("/api/lobbies", lobbyHandler.Create)
	router.POST("/api/lobbies/:lobbyID/join", lobbyHandler.Join)
	router.GET("/ws/rooms/:roomID", realtimeHandler.ServeWebSocket)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return router
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
