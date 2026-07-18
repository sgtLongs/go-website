package realtime

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sgtLongs/go-website/internal/game"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second
	maxMessageSize = 4096
)

// Client represents one browser tab, not necessarily one human account.
type Client struct {
	participant Participant
	room        *Room
	connection  *websocket.Conn
	send        chan []byte
}

type clientCommand struct {
	Type      string        `json:"type"`
	PlayerIDs []string      `json:"playerIds"`
	Choice    bool          `json:"choice"`
	Settings  game.Settings `json:"settings"`
}

func (c *Client) readPump() {
	defer func() {
		select {
		case c.room.unregister <- c:
		case <-c.room.done:
		}
		c.connection.Close()
	}()

	c.connection.SetReadLimit(maxMessageSize)
	_ = c.connection.SetReadDeadline(time.Now().Add(pongWait))
	c.connection.SetPongHandler(func(string) error {
		return c.connection.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, message, err := c.connection.ReadMessage()
		if err != nil {
			return
		}
		var command clientCommand
		if json.Unmarshal(message, &command) != nil {
			continue
		}
		switch command.Type {
		case "leave_room", "start_game", "update_game_settings", "cancel_game_start", "end_game", "confirm_game_start", "confirm_role", "confirm_proposal_result", "propose_quest", "vote_proposal", "play_quest", "assassinate":
			c.room.commands <- roomCommand{
				client: c, kind: command.Type,
				playerIDs: command.PlayerIDs, choice: command.Choice, settings: command.Settings,
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.connection.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.connection.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.connection.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.connection.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.connection.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
