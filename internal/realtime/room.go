package realtime

import (
	"encoding/json"
	"errors"
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
	id         string
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	commands   chan roomCommand
	game       *game.Engine
	count      atomic.Int64
}

func newRoom(id string) *Room {
	return &Room{
		id:         id,
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 32),
		commands:   make(chan roomCommand),
		game:       game.New(),
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
	if state.Phase != "" {
		snapshot.Game = &state
		if role, assigned := r.game.RoleFor(client.participant.ID); assigned {
			snapshot.Role = string(role)
		}
	}
	r.queueEvent(client, Event{Type: "presence_snapshot", RoomID: r.id, Data: snapshot})
}

func (r *Room) handleCommand(command roomCommand) {
	var err error
	switch command.kind {
	case "start_game":
		err = r.startGame(command.client)
	case "propose_quest":
		err = r.game.ProposeQuest(command.client.participant.ID, command.playerIDs)
	case "vote_proposal":
		_, err = r.game.VoteOnProposal(command.client.participant.ID, command.choice)
	case "play_quest":
		_, err = r.game.PlayQuestCard(command.client.participant.ID, command.choice)
	default:
		return
	}
	if err != nil {
		r.queueGameError(command.client, err)
		return
	}
	if command.kind != "start_game" {
		r.broadcastEvent("game_updated", r.game.Snapshot())
	}
}

func (r *Room) startGame(client *Client) error {
	if !client.participant.Host {
		r.queueError(client, "Only the host can start a game.")
		return nil
	}
	players := make([]game.Player, 0, len(r.clients))
	for connected := range r.clients {
		players = append(players, game.Player{ID: connected.participant.ID, Name: connected.participant.Name})
	}
	// Make the rotation queue stable; the engine independently randomizes the
	// first captain, so map iteration order never affects the game.
	sort.Slice(players, func(i, j int) bool { return players[i].ID < players[j].ID })
	started, err := r.game.Start(players)
	if err != nil {
		return err
	}
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
		message = "Choose exactly three different players."
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
