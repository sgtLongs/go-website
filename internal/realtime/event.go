package realtime

import "github.com/sgtLongs/go-website/internal/game"

type Event struct {
	Type   string `json:"type"`
	RoomID string `json:"roomId"`
	Data   any    `json:"data"`
}

type Participant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host bool   `json:"host"`
}

type presenceSnapshot struct {
	Participants                  []Participant  `json:"participants"`
	Game                          *game.Snapshot `json:"game,omitempty"`
	Role                          string         `json:"role,omitempty"`
	PendingRoleConfirmations      []game.Player  `json:"pendingRoleConfirmations,omitempty"`
	PendingProposalConfirmations  []game.Player  `json:"pendingProposalConfirmations,omitempty"`
	PendingGameStartConfirmations []game.Player  `json:"pendingGameStartConfirmations,omitempty"`
	GameStartPlayers              []game.Player  `json:"gameStartPlayers,omitempty"`
	GameStarting                  bool           `json:"gameStarting,omitempty"`
	Host                          bool           `json:"host"`
	PlayerID                      string         `json:"playerId"`
}
