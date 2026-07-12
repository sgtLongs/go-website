package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/sgtLongs/go-website/internal/game"
)

type roomCommand struct {
	client    *Client
	kind      string
	playerIDs []string
	choice    bool
}

type Room struct {
	id                     string
	clients                map[*Client]struct{}
	register               chan *Client
	unregister             chan *Client
	broadcast              chan []byte
	commands               chan roomCommand
	game                   *game.Engine
	roleConfirmations      map[string]bool
	proposalConfirmations  map[string]bool
	proposalResultPending  bool
	gameStarting           bool
	gameStartPlayers       []game.Player
	gameStartConfirmations map[string]bool
	count                  atomic.Int64
}

func newRoom(id string) *Room {
	return &Room{
		id:                     id,
		clients:                make(map[*Client]struct{}),
		register:               make(chan *Client),
		unregister:             make(chan *Client),
		broadcast:              make(chan []byte, 32),
		commands:               make(chan roomCommand),
		game:                   game.New(),
		roleConfirmations:      make(map[string]bool),
		proposalConfirmations:  make(map[string]bool),
		gameStartConfirmations: make(map[string]bool),
	}
}

func (r *Room) run() {
	for {
		select {
		case client := <-r.register:
			r.clients[client] = struct{}{}
			r.count.Add(1)
			r.sendSnapshot(client)
			r.broadcastEvent("user_joined", client.participant)

		case client := <-r.unregister:
			if _, exists := r.clients[client]; !exists {
				continue
			}
			delete(r.clients, client)
			r.count.Add(-1)
			close(client.send)
			r.broadcastEvent("user_left", client.participant)
			if r.gameStarting && r.gameStartHasPlayer(client.participant.ID) {
				r.cancelGameStart("Game start cancelled because a player disconnected.")
			}
			if r.game.Active() && r.game.HasPlayer(client.participant.ID) {
				r.game.Cancel()
				r.broadcastEvent("game_cancelled", map[string]string{"message": "The game ended because a player disconnected."})
			}

		case message := <-r.broadcast:
			for client := range r.clients {
				if !r.queue(client, message) {
					delete(r.clients, client)
					close(client.send)
				}
			}

		case command := <-r.commands:
			r.handleCommand(command)
		}
	}
}

func (r *Room) sendSnapshot(client *Client) {
	participants := make([]Participant, 0, len(r.clients))
	for connected := range r.clients {
		participants = append(participants, connected.participant)
	}
	sort.Slice(participants, func(i, j int) bool { return participants[i].ID < participants[j].ID })
	snapshot := presenceSnapshot{
		Participants: participants,
		Host:         client.participant.Host,
		PlayerID:     client.participant.ID,
	}
	state := r.game.Snapshot()
	snapshot.GameStarting = r.gameStarting
	if r.gameStarting {
		snapshot.PendingGameStartConfirmations = r.pendingGameStartConfirmations()
		snapshot.GameStartPlayers = append([]game.Player(nil), r.gameStartPlayers...)
	}
	if state.Phase != "" {
		snapshot.Game = &state
		if role, assigned := r.game.RoleFor(client.participant.ID); assigned {
			snapshot.Role = string(role)
		}
		snapshot.PendingRoleConfirmations = r.pendingRoleConfirmations()
		if r.proposalResultPending {
			snapshot.PendingProposalConfirmations = r.pendingProposalConfirmations()
		}
	}
	r.queueEvent(client, Event{Type: "presence_snapshot", RoomID: r.id, Data: snapshot})
}

func (r *Room) handleCommand(command roomCommand) {
	var err error
	switch command.kind {
	case "start_game":
		err = r.prepareGame(command.client)
	case "confirm_game_start":
		if r.gameStarting && r.gameStartHasPlayer(command.client.participant.ID) {
			r.gameStartConfirmations[command.client.participant.ID] = true
			if len(r.pendingGameStartConfirmations()) == 0 {
				err = r.launchGame(r.gameStartPlayers)
			} else {
				r.broadcastGameStartConfirmations()
				return
			}
		} else {
			return
		}
	case "confirm_role":
		if r.game.Active() && r.game.HasPlayer(command.client.participant.ID) {
			r.roleConfirmations[command.client.participant.ID] = true
			r.broadcastRoleConfirmations()
		}
		return
	case "confirm_proposal_result":
		if r.proposalResultPending && r.game.HasPlayer(command.client.participant.ID) {
			r.proposalConfirmations[command.client.participant.ID] = true
			if len(r.pendingProposalConfirmations()) == 0 {
				r.proposalResultPending = false
			}
			r.broadcastProposalConfirmations()
		}
		return
	case "propose_quest":
		if len(r.pendingRoleConfirmations()) > 0 || r.proposalResultPending {
			r.queueError(command.client, "Wait for every player to finish the current confirmation.")
			return
		}
		err = r.game.ProposeQuest(command.client.participant.ID, command.playerIDs)
	case "vote_proposal":
		var completed bool
		completed, err = r.game.VoteOnProposal(command.client.participant.ID, command.choice)
		if err == nil && completed {
			r.proposalResultPending = true
			r.proposalConfirmations = make(map[string]bool)
		}
	case "play_quest":
		if r.proposalResultPending {
			r.queueError(command.client, "Wait for every player to acknowledge the team vote.")
			return
		}
		_, err = r.game.PlayQuestCard(command.client.participant.ID, command.choice)
	default:
		return
	}
	if err != nil {
		r.queueGameError(command.client, err)
		return
	}
	if command.kind != "start_game" && command.kind != "confirm_game_start" {
		r.broadcastEvent("game_updated", r.game.Snapshot())
		if command.kind == "vote_proposal" && r.proposalResultPending {
			r.broadcastProposalConfirmations()
		}
	}
}

func (r *Room) prepareGame(client *Client) error {
	if !client.participant.Host {
		r.queueError(client, "Only the host can start a game.")
		return nil
	}
	if r.game.Active() || r.gameStarting {
		return game.ErrAlreadyActive
	}
	players := r.connectedPlayers()
	if len(players) < game.MinPlayers {
		return game.ErrNotEnoughPlayers
	}
	r.gameStarting = true
	r.gameStartPlayers = players
	r.gameStartConfirmations = make(map[string]bool, len(players))
	r.broadcastEvent("game_starting", map[string]any{
		"players": r.gameStartPlayers, "pendingPlayers": r.pendingGameStartConfirmations(),
	})
	return nil
}

func (r *Room) startGame(client *Client) error {
	if !client.participant.Host {
		r.queueError(client, "Only the host can start a game.")
		return nil
	}
	return r.launchGame(r.connectedPlayers())
}

func (r *Room) connectedPlayers() []game.Player {
	players := make([]game.Player, 0, len(r.clients))
	for connected := range r.clients {
		players = append(players, game.Player{ID: connected.participant.ID, Name: connected.participant.Name})
	}
	// Make the rotation queue stable; the engine independently randomizes the
	// first captain, so map iteration order never affects the game.
	sort.Slice(players, func(i, j int) bool { return players[i].ID < players[j].ID })
	return players
}

func (r *Room) launchGame(players []game.Player) error {
	started, err := r.game.Start(players)
	if err != nil {
		return err
	}
	r.roleConfirmations = make(map[string]bool, len(players))
	r.proposalResultPending = false
	r.proposalConfirmations = make(map[string]bool)
	r.gameStarting = false
	r.gameStartPlayers = nil
	r.gameStartConfirmations = make(map[string]bool)
	r.broadcastEvent("game_started", started.State)
	for connected := range r.clients {
		role := started.Roles[connected.participant.ID]
		r.queueEvent(connected, Event{
			Type: "role_assigned", RoomID: r.id,
			Data: map[string]string{"role": string(role)},
		})
	}
	return nil
}

func (r *Room) gameStartHasPlayer(id string) bool {
	for _, player := range r.gameStartPlayers {
		if player.ID == id {
			return true
		}
	}
	return false
}

func (r *Room) pendingGameStartConfirmations() []game.Player {
	pending := make([]game.Player, 0, len(r.gameStartPlayers))
	for _, player := range r.gameStartPlayers {
		if !r.gameStartConfirmations[player.ID] {
			pending = append(pending, player)
		}
	}
	return pending
}

func (r *Room) broadcastGameStartConfirmations() {
	r.broadcastEvent("game_start_confirmations_updated", map[string]any{"pendingPlayers": r.pendingGameStartConfirmations()})
}

func (r *Room) cancelGameStart(message string) {
	r.gameStarting = false
	r.gameStartPlayers = nil
	r.gameStartConfirmations = make(map[string]bool)
	r.broadcastEvent("game_start_cancelled", map[string]string{"message": message})
}

func (r *Room) pendingRoleConfirmations() []game.Player {
	state := r.game.Snapshot()
	pending := make([]game.Player, 0, len(state.Players))
	for _, player := range state.Players {
		if !r.roleConfirmations[player.ID] {
			pending = append(pending, player)
		}
	}
	return pending
}

func (r *Room) broadcastRoleConfirmations() {
	r.broadcastEvent("role_confirmations_updated", map[string]any{
		"pendingPlayers": r.pendingRoleConfirmations(),
	})
}

func (r *Room) pendingProposalConfirmations() []game.Player {
	state := r.game.Snapshot()
	pending := make([]game.Player, 0, len(state.Players))
	for _, player := range state.Players {
		if !r.proposalConfirmations[player.ID] {
			pending = append(pending, player)
		}
	}
	return pending
}

func (r *Room) broadcastProposalConfirmations() {
	r.broadcastEvent("proposal_result_confirmations_updated", map[string]any{
		"pendingPlayers": r.pendingProposalConfirmations(),
		"waiting":        r.proposalResultPending,
	})
}

func (r *Room) queueGameError(client *Client, err error) {
	message := "That action could not be completed."
	switch {
	case errors.Is(err, game.ErrAlreadyActive):
		message = "A game is already running."
	case errors.Is(err, game.ErrNotEnoughPlayers):
		message = "At least three players are needed."
	case errors.Is(err, game.ErrNotCaptain):
		message = "Only the current captain can choose the quest team."
	case errors.Is(err, game.ErrInvalidQuest):
		message = fmt.Sprintf("Choose exactly %d different players.", r.game.Snapshot().QuestSize)
	case errors.Is(err, game.ErrMissingQuestRule):
		message = "No quest-size rule is configured for this lobby."
	case errors.Is(err, game.ErrNotProposalVoter):
		message = "Only players in this game may vote on the proposal."
	case errors.Is(err, game.ErrAlreadyVoted):
		message = "You have already submitted your choice."
	case errors.Is(err, game.ErrNotOnQuest):
		message = "You are not on this quest."
	case errors.Is(err, game.ErrInnocentCannotFail):
		message = "Only traitors may play a fail card."
	case errors.Is(err, game.ErrWrongPhase), errors.Is(err, game.ErrNotActive):
		message = "That action is not available right now."
	}
	r.queueError(client, message)
}

func (r *Room) queueError(client *Client, message string) {
	r.queueEvent(client, Event{Type: "error", RoomID: r.id, Data: map[string]string{"message": message}})
}

func (r *Room) broadcastEvent(eventType string, data any) {
	message, err := json.Marshal(Event{Type: eventType, RoomID: r.id, Data: data})
	if err != nil {
		return
	}
	for client := range r.clients {
		r.queue(client, message)
	}
}

func (r *Room) queueEvent(client *Client, event Event) {
	message, err := json.Marshal(event)
	if err == nil {
		r.queue(client, message)
	}
}

func (r *Room) queue(client *Client, message []byte) bool {
	select {
	case client.send <- message:
		return true
	default:
		return false
	}
}
