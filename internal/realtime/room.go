package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync/atomic"
	"time"

	"github.com/sgtLongs/go-website/internal/game"
	"github.com/sgtLongs/go-website/internal/persistence"
)

const (
	roomSchemaVersion = 1
	defaultEmptyGrace = 5 * time.Minute
)

var errOnlyHost = errors.New("only the host can start a game")

type roomCommand struct {
	client    *Client
	kind      string
	playerIDs []string
	choice    bool
}

type persistedRoom struct {
	SchemaVersion          int                 `json:"schemaVersion"`
	ID                     string              `json:"id"`
	Game                   game.PersistedState `json:"game"`
	RoleConfirmations      map[string]bool     `json:"roleConfirmations,omitempty"`
	ProposalConfirmations  map[string]bool     `json:"proposalConfirmations,omitempty"`
	ProposalResultPending  bool                `json:"proposalResultPending"`
	GameStarting           bool                `json:"gameStarting"`
	GameStartPlayers       []game.Player       `json:"gameStartPlayers,omitempty"`
	GameStartConfirmations map[string]bool     `json:"gameStartConfirmations,omitempty"`
	UpdatedAt              time.Time           `json:"updatedAt"`
}

type Room struct {
	id                     string
	clients                map[*Client]struct{}
	connections            map[string]*Client
	register               chan *Client
	unregister             chan *Client
	commands               chan roomCommand
	done                   chan struct{}
	game                   *game.Engine
	roleConfirmations      map[string]bool
	proposalConfirmations  map[string]bool
	proposalResultPending  bool
	gameStarting           bool
	gameStartPlayers       []game.Player
	gameStartConfirmations map[string]bool
	count                  atomic.Int64
	gameStartCountdown     uint64
	onEmpty                func(*Room)
	store                  *persistence.Store
	emptyGrace             time.Duration
	startsEmpty            bool
}

func newRoom(id string, onEmpty func(*Room)) *Room {
	return newRoomWithStore(id, onEmpty, nil)
}

func newRoomWithStore(id string, onEmpty func(*Room), store *persistence.Store) *Room {
	return &Room{
		id:                     id,
		clients:                make(map[*Client]struct{}),
		connections:            make(map[string]*Client),
		register:               make(chan *Client),
		unregister:             make(chan *Client),
		commands:               make(chan roomCommand, 16),
		done:                   make(chan struct{}),
		game:                   game.New(),
		roleConfirmations:      make(map[string]bool),
		proposalConfirmations:  make(map[string]bool),
		gameStartConfirmations: make(map[string]bool),
		onEmpty:                onEmpty,
		store:                  store,
		emptyGrace:             defaultEmptyGrace,
	}
}

func restoreRoom(encoded []byte, onEmpty func(*Room), store *persistence.Store) (*Room, error) {
	var state persistedRoom
	if err := json.Unmarshal(encoded, &state); err != nil {
		return nil, fmt.Errorf("decode room: %w", err)
	}
	if state.SchemaVersion != roomSchemaVersion || !ValidRoomID(state.ID) {
		return nil, errors.New("persisted room has an unsupported schema or invalid ID")
	}
	room := newRoomWithStore(state.ID, onEmpty, store)
	if err := room.restore(state); err != nil {
		return nil, err
	}
	// A process restart disconnects every socket. Ready-up is intentionally
	// reset so an old all-ready snapshot cannot become permanently stuck.
	if room.gameStarting {
		room.gameStartConfirmations = make(map[string]bool)
	}
	room.startsEmpty = true
	return room, nil
}

func (r *Room) run() {
	defer close(r.done)
	var emptyTimer *time.Timer
	var emptyDeadline <-chan time.Time
	startEmptyTimer := func() {
		if emptyTimer != nil {
			emptyTimer.Stop()
		}
		emptyTimer = time.NewTimer(r.emptyGrace)
		emptyDeadline = emptyTimer.C
	}
	stopEmptyTimer := func() {
		if emptyTimer != nil {
			emptyTimer.Stop()
		}
		emptyDeadline = nil
	}
	if r.startsEmpty {
		startEmptyTimer()
	}

	for {
		select {
		case client := <-r.register:
			stopEmptyTimer()
			if previous := r.connections[client.participant.ID]; previous != nil && previous != client {
				delete(r.clients, previous)
				close(previous.send)
			} else {
				r.count.Add(1)
			}
			r.clients[client] = struct{}{}
			r.connections[client.participant.ID] = client
			if err := r.persist(); err != nil {
				log.Printf("persist room %s on connection: %v", r.id, err)
				r.queueError(client, "The room could not be saved. Please try reconnecting.")
			}
			r.sendSnapshot(client)
			r.broadcastEvent("user_joined", client.participant)

		case client := <-r.unregister:
			if !r.disconnect(client, true) {
				continue
			}
			if len(r.clients) == 0 {
				startEmptyTimer()
			}

		case <-emptyDeadline:
			if len(r.clients) != 0 {
				continue
			}
			if r.onEmpty != nil {
				r.onEmpty(r)
			}
			return

		case command := <-r.commands:
			r.handleCommand(command)
		}
	}
}

func (r *Room) disconnect(client *Client, announce bool) bool {
	if r.connections[client.participant.ID] != client {
		return false
	}
	delete(r.connections, client.participant.ID)
	delete(r.clients, client)
	r.count.Add(-1)
	close(client.send)
	if announce {
		r.broadcastEvent("user_left", client.participant)
	}
	if r.gameStarting && r.gameStartHasPlayer(client.participant.ID) {
		wasReady := r.gameStartConfirmations[client.participant.ID]
		r.gameStartConfirmations[client.participant.ID] = false
		r.gameStartCountdown++
		if err := r.persist(); err != nil {
			log.Printf("persist room %s after disconnect: %v", r.id, err)
		}
		if wasReady {
			player := game.Player{ID: client.participant.ID, Name: client.participant.Name}
			r.broadcastGameStartConfirmations(&player, false)
		} else {
			r.broadcastGameStartConfirmations(nil, false)
		}
	}
	return true
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
		snapshot.GameStartConfirmed = r.gameStartConfirmations[client.participant.ID]
	}
	if state.Phase != "" {
		snapshot.Game = &state
		if role, assigned := r.game.RoleFor(client.participant.ID); assigned {
			snapshot.Role = string(role)
			snapshot.KnownRoles = r.game.KnownRolesFor(client.participant.ID)
			snapshot.RoleConfirmed = r.roleConfirmations[client.participant.ID]
		}
		snapshot.ProposalVoteSubmitted = r.game.HasVoted(client.participant.ID)
		snapshot.QuestCardSubmitted = r.game.HasPlayedQuestCard(client.participant.ID)
		snapshot.PendingRoleConfirmations = r.pendingRoleConfirmations()
		if r.proposalResultPending {
			snapshot.PendingProposalConfirmations = r.pendingProposalConfirmations()
			snapshot.ProposalResultConfirmed = r.proposalConfirmations[client.participant.ID]
		}
	}
	r.queueEvent(client, Event{Type: "presence_snapshot", RoomID: r.id, Data: snapshot})
}

func (r *Room) handleCommand(command roomCommand) {
	if command.client != nil {
		current, tracked := r.connections[command.client.participant.ID]
		if (tracked && current != command.client) || (!tracked && !r.clientExists(command.client)) {
			return
		}
	}
	before := r.state()
	var err error
	switch command.kind {
	case "start_game":
		err = r.prepareGame(command.client)
		if err == nil && r.saveOrRestore(before, command.client) {
			r.broadcastEvent("game_starting", map[string]any{
				"players": r.gameStartPlayers, "pendingPlayers": r.pendingGameStartConfirmations(),
			})
		}
		if err != nil {
			r.queueGameError(command.client, err)
		}
		return
	case "end_game":
		if !command.client.participant.Host {
			r.queueError(command.client, "Only the host can end the game.")
			return
		}
		r.game.Cancel()
		r.gameStarting = false
		r.gameStartPlayers = nil
		r.gameStartConfirmations = make(map[string]bool)
		r.roleConfirmations = make(map[string]bool)
		r.proposalResultPending = false
		r.proposalConfirmations = make(map[string]bool)
		if r.saveOrRestore(before, command.client) {
			r.broadcastEvent("game_cancelled", map[string]string{"message": "The host ended the game."})
		}
		return
	case "confirm_game_start":
		if !r.gameStarting || !r.gameStartHasPlayer(command.client.participant.ID) {
			return
		}
		playerID := command.client.participant.ID
		wasReady := r.gameStartConfirmations[playerID]
		r.gameStartConfirmations[playerID] = !wasReady
		var unreadied *game.Player
		countdownReady := len(r.pendingGameStartConfirmations()) == 0
		if countdownReady {
			r.gameStartCountdown++
		} else if wasReady {
			r.gameStartCountdown++
			for _, player := range r.gameStartPlayers {
				if player.ID == playerID {
					copy := player
					unreadied = &copy
					break
				}
			}
		}
		if !r.saveOrRestore(before, command.client) {
			return
		}
		r.broadcastGameStartConfirmations(unreadied, countdownReady)
		if countdownReady {
			countdown := r.gameStartCountdown
			time.AfterFunc(3*time.Second, func() {
				select {
				case r.commands <- roomCommand{kind: "launch_game", playerIDs: []string{fmt.Sprint(countdown)}}:
				case <-r.done:
				}
			})
		}
		return
	case "launch_game":
		var countdown uint64
		if len(command.playerIDs) != 1 {
			return
		}
		if _, scanErr := fmt.Sscan(command.playerIDs[0], &countdown); scanErr != nil {
			return
		}
		if r.gameStarting && countdown == r.gameStartCountdown && len(r.pendingGameStartConfirmations()) == 0 {
			if err := r.launchGame(r.gameStartPlayers); err != nil {
				log.Printf("launch game in room %s: %v", r.id, err)
			}
		}
		return
	case "confirm_role":
		if r.game.Active() && r.game.HasPlayer(command.client.participant.ID) {
			r.roleConfirmations[command.client.participant.ID] = true
			if r.saveOrRestore(before, command.client) {
				r.broadcastRoleConfirmations()
			}
		}
		return
	case "confirm_proposal_result":
		if r.proposalResultPending && r.game.HasPlayer(command.client.participant.ID) {
			r.proposalConfirmations[command.client.participant.ID] = true
			if len(r.pendingProposalConfirmations()) == 0 {
				r.proposalResultPending = false
			}
			if r.saveOrRestore(before, command.client) {
				r.broadcastProposalConfirmations()
			}
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
	case "assassinate":
		if len(command.playerIDs) != 1 {
			err = game.ErrInvalidTarget
		} else {
			_, err = r.game.Assassinate(command.client.participant.ID, command.playerIDs[0])
		}
	default:
		return
	}
	if err != nil {
		r.queueGameError(command.client, err)
		return
	}
	if !r.saveOrRestore(before, command.client) {
		return
	}
	r.broadcastEvent("game_updated", r.game.Snapshot())
	if command.kind == "vote_proposal" && r.proposalResultPending {
		r.broadcastProposalConfirmations()
	}
}

func (r *Room) clientExists(client *Client) bool {
	_, exists := r.clients[client]
	return exists
}

func (r *Room) prepareGame(client *Client) error {
	if !client.participant.Host {
		return errOnlyHost
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
	sort.Slice(players, func(i, j int) bool { return players[i].ID < players[j].ID })
	return players
}

func (r *Room) launchGame(players []game.Player) error {
	before := r.state()
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
	if !r.saveOrRestore(before, nil) {
		return errors.New("persist game start")
	}
	r.broadcastEvent("game_started", started.State)
	for connected := range r.clients {
		role, assigned := started.Roles[connected.participant.ID]
		if !assigned {
			continue
		}
		r.queueEvent(connected, Event{
			Type: "role_assigned", RoomID: r.id,
			Data: map[string]any{
				"role":       string(role),
				"knownRoles": r.game.KnownRolesFor(connected.participant.ID),
			},
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

func (r *Room) broadcastGameStartConfirmations(unreadied *game.Player, countdown bool) {
	data := map[string]any{"pendingPlayers": r.pendingGameStartConfirmations(), "countdown": countdown}
	if unreadied != nil {
		data["unreadiedPlayer"] = unreadied
	}
	r.broadcastEvent("game_start_confirmations_updated", data)
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

func (r *Room) state() persistedRoom {
	return persistedRoom{
		SchemaVersion: roomSchemaVersion, ID: r.id, Game: r.game.Export(),
		RoleConfirmations:     copyConfirmations(r.roleConfirmations),
		ProposalConfirmations: copyConfirmations(r.proposalConfirmations),
		ProposalResultPending: r.proposalResultPending,
		GameStarting:          r.gameStarting, GameStartPlayers: append([]game.Player(nil), r.gameStartPlayers...),
		GameStartConfirmations: copyConfirmations(r.gameStartConfirmations), UpdatedAt: time.Now().UTC(),
	}
}

func (r *Room) restore(state persistedRoom) error {
	if state.ID != r.id {
		return errors.New("persisted room ID does not match its storage key")
	}
	if err := r.game.Restore(state.Game); err != nil {
		return fmt.Errorf("restore game: %w", err)
	}
	r.roleConfirmations = copyConfirmations(state.RoleConfirmations)
	r.proposalConfirmations = copyConfirmations(state.ProposalConfirmations)
	r.proposalResultPending = state.ProposalResultPending
	r.gameStarting = state.GameStarting
	r.gameStartPlayers = append([]game.Player(nil), state.GameStartPlayers...)
	r.gameStartConfirmations = copyConfirmations(state.GameStartConfirmations)
	return nil
}

func (r *Room) persist() error {
	if r.store == nil {
		return nil
	}
	encoded, err := json.Marshal(r.state())
	if err != nil {
		return err
	}
	return r.store.Put(persistence.RoomsBucket, []byte(r.id), encoded)
}

func (r *Room) saveOrRestore(before persistedRoom, client *Client) bool {
	if err := r.persist(); err != nil {
		if restoreErr := r.restore(before); restoreErr != nil {
			log.Printf("restore room %s after persistence failure: %v", r.id, restoreErr)
		}
		log.Printf("persist room %s: %v", r.id, err)
		if client != nil {
			r.queueError(client, "The room could not save that action. Please try again.")
		} else {
			r.broadcastEvent("error", map[string]string{"message": "The room could not save that action. Please try again."})
		}
		return false
	}
	return true
}

func copyConfirmations(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for id, confirmed := range source {
		result[id] = confirmed
	}
	return result
}

func (r *Room) queueGameError(client *Client, err error) {
	message := "That action could not be completed."
	switch {
	case errors.Is(err, game.ErrAlreadyActive):
		message = "A game is already running."
	case errors.Is(err, game.ErrNotEnoughPlayers):
		message = "At least three players are needed."
	case errors.Is(err, errOnlyHost):
		message = "Only the host can start a game."
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
	case errors.Is(err, game.ErrNotAssassin):
		message = "Only the Assassin may assassinate a player."
	case errors.Is(err, game.ErrAssassinationUsed):
		message = "The Assassin has already made their one attempt."
	case errors.Is(err, game.ErrInvalidTarget):
		message = "Choose another player to assassinate."
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
	if client == nil {
		return
	}
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
