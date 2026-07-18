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
	Participants                  []Participant        `json:"participants"`
	Game                          *game.Snapshot       `json:"game,omitempty"`
	Role                          string               `json:"role,omitempty"`
	KnownRoles                    map[string]game.Role `json:"knownRoles,omitempty"`
	PendingRoleConfirmations      []game.Player        `json:"pendingRoleConfirmations,omitempty"`
	PendingProposalConfirmations  []game.Player        `json:"pendingProposalConfirmations,omitempty"`
	PendingGameStartConfirmations []game.Player        `json:"pendingGameStartConfirmations,omitempty"`
	GameStartPlayers              []game.Player        `json:"gameStartPlayers,omitempty"`
	GameStarting                  bool                 `json:"gameStarting,omitempty"`
	GameStartConfirmed            bool                 `json:"gameStartConfirmed,omitempty"`
	GameSettings                  *game.Settings       `json:"gameSettings,omitempty"`
	RoleConfirmed                 bool                 `json:"roleConfirmed,omitempty"`
	ProposalVoteSubmitted         bool                 `json:"proposalVoteSubmitted,omitempty"`
	QuestCardSubmitted            bool                 `json:"questCardSubmitted,omitempty"`
	ProposalResultConfirmed       bool                 `json:"proposalResultConfirmed,omitempty"`
	Host                          bool                 `json:"host"`
	PlayerID                      string               `json:"playerId"`
}
