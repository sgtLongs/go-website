package main

import (
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sgtLongs/go-website/frontend"
	"github.com/sgtLongs/go-website/internal/realtime"
)

func main() {
	var router *gin.Engine = newRouter()
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

func newRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	manager := realtime.NewManager()
	handler := realtime.NewHandler(manager)
	assets, err := fs.Sub(frontend.Files, "assets")
	if err != nil {
		panic(err)
	}

	router.GET("/", func(c *gin.Context) {
		c.FileFromFS("html/index.html", http.FS(frontend.Files))
	})
	router.StaticFS("/assets", http.FS(assets))
	router.GET("/room/:roomID", func(c *gin.Context) {
		if !realtime.ValidRoomID(c.Param("roomID")) {
			c.String(http.StatusBadRequest, "invalid room ID")
			return
		}
		c.FileFromFS("html/room.html", http.FS(frontend.Files))
	})
	router.GET("/ws/rooms/:roomID", handler.ServeWebSocket)
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
