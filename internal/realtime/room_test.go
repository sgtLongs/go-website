package realtime

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/sgtLongs/go-website/internal/game"
	"github.com/sgtLongs/go-website/internal/persistence"
)

func TestPersistedRoomRestoresRoleAndConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rooms.db")
	store, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	room := newRoomWithStore("game-room", nil, store)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
	}
	if err := room.startGame(clients[0]); err != nil {
		t.Fatal(err)
	}
	var originalRole string
	for _, client := range clients {
		_ = receiveEvent(t, client)
		roleEvent := receiveEvent(t, client)
		if client == clients[0] {
			originalRole = roleEvent.Data.(map[string]any)["role"].(string)
		}
	}
	room.handleCommand(roomCommand{client: clients[0], kind: "confirm_role"})
	for _, client := range clients {
		_ = receiveEvent(t, client)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	encoded, exists, err := reopened.Get(persistence.RoomsBucket, []byte("game-room"))
	if err != nil || !exists {
		t.Fatalf("load persisted room = %v, %v", exists, err)
	}
	restored, err := restoreRoom(encoded, nil, reopened)
	if err != nil {
		t.Fatal(err)
	}
	rejoined := testClient(restored, "Host", true)
	restored.sendSnapshot(rejoined)
	event := receiveEvent(t, rejoined)
	encodedSnapshot, err := json.Marshal(event.Data)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot presenceSnapshot
	if err := json.Unmarshal(encodedSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.PlayerID != "Host" || snapshot.Role != originalRole || !snapshot.RoleConfirmed {
		t.Fatalf("restored private snapshot = %#v; want player Host, role %q, confirmed", snapshot, originalRole)
	}
}

func TestDisconnectKeepsActiveGameOpen(t *testing.T) {
	room := newRoom("game-room", nil)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
		room.connections[client.participant.ID] = client
		room.count.Add(1)
	}
	if err := room.startGame(clients[0]); err != nil {
		t.Fatal(err)
	}
	for _, client := range clients {
		_ = receiveEvent(t, client)
		_ = receiveEvent(t, client)
	}
	if !room.disconnect(clients[1], true) {
		t.Fatal("connected player was not disconnected")
	}
	if !room.game.Active() || !room.game.HasPlayer(clients[1].participant.ID) {
		t.Fatal("disconnect removed the player or cancelled the active game")
	}
	if room.count.Load() != 2 {
		t.Fatalf("connected count = %d, want 2", room.count.Load())
	}
}

func TestEmptyRoomClosesAfterGracePeriod(t *testing.T) {
	closed := make(chan struct{}, 1)
	room := newRoom("game-room", func(*Room) { closed <- struct{}{} })
	room.emptyGrace = 10 * time.Millisecond
	go room.run()
	client := testClient(room, "Host", true)
	room.register <- client
	_ = receiveEvent(t, client)
	_ = receiveEvent(t, client)
	room.unregister <- client
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("empty room did not close after its grace period")
	}
}

func TestHostStartsGameBroadcastsStateAndAssignments(t *testing.T) {
	room := newRoom("game-room", nil)
	host := testClient(room, "Host", true)
	guestOne := testClient(room, "Guest One", false)
	guestTwo := testClient(room, "Guest Two", false)
	room.clients[host] = struct{}{}
	room.clients[guestOne] = struct{}{}
	room.clients[guestTwo] = struct{}{}

	if err := room.startGame(host); err != nil {
		t.Fatal(err)
	}
	if !room.game.Active() {
		t.Fatal("game should be active")
	}

	traitors, merlins := 0, 0
	for _, client := range []*Client{host, guestOne, guestTwo} {
		started := receiveEvent(t, client)
		if started.Type != "game_started" {
			t.Fatalf("first event = %q, want game_started", started.Type)
		}
		role := receiveEvent(t, client)
		if role.Type != "role_assigned" {
			t.Fatalf("second event = %q, want role_assigned", role.Type)
		}
		data := role.Data.(map[string]any)
		known := data["knownRoles"].(map[string]any)
		switch data["role"] {
		case "assassin":
			traitors++
			if len(known) != 1 || known[client.participant.ID] != "assassin" {
				t.Fatalf("Assassin role markers = %#v", known)
			}
		case "merlin":
			merlins++
			if len(known) != 2 || known[client.participant.ID] != "merlin" {
				t.Fatalf("Merlin role markers = %#v", known)
			}
			visibleTraitors := 0
			for _, marker := range known {
				if marker == "traitor" {
					visibleTraitors++
				}
			}
			if visibleTraitors != 1 {
				t.Fatalf("Merlin sees %d traitors in %#v, want 1", visibleTraitors, known)
			}
		case "innocent":
			if len(known) != 0 {
				t.Fatalf("ordinary innocent role markers = %#v, want none", known)
			}
		}
	}
	if traitors != 1 || merlins != 1 {
		t.Fatalf("special role counts = %d Assassins and %d Merlins, want 1 each", traitors, merlins)
	}
}

func TestOnlyHostCanStartGame(t *testing.T) {
	room := newRoom("game-room", nil)
	host := testClient(room, "Host", true)
	guestOne := testClient(room, "Guest One", false)
	guestTwo := testClient(room, "Guest Two", false)
	room.clients[host] = struct{}{}
	room.clients[guestOne] = struct{}{}
	room.clients[guestTwo] = struct{}{}

	if err := room.startGame(guestOne); err != nil {
		t.Fatal(err)
	}
	event := receiveEvent(t, guestOne)
	if event.Type != "error" || room.game.Active() {
		t.Fatalf("event = %#v; active = %v", event, room.game.Active())
	}
}

func TestGameStartsOnlyAfterEveryPlayerReadies(t *testing.T) {
	room := newRoom("game-room", nil)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
	}

	room.handleCommand(roomCommand{client: clients[0], kind: "start_game"})
	if room.game.Active() {
		t.Fatal("game should remain inactive while players ready up")
	}
	for _, client := range clients {
		if event := receiveEvent(t, client); event.Type != "game_starting" {
			t.Fatalf("event = %q, want game_starting", event.Type)
		}
	}

	for i, client := range clients {
		room.handleCommand(roomCommand{client: client, kind: "confirm_game_start"})
		if room.game.Active() {
			t.Fatalf("game became active before the countdown after %d ready players", i+1)
		}
		for _, recipient := range clients {
			if event := receiveEvent(t, recipient); event.Type != "game_start_confirmations_updated" {
				t.Fatalf("event = %q, want game_start_confirmations_updated", event.Type)
			}
		}
	}
	room.handleCommand(roomCommand{kind: "launch_game", playerIDs: []string{"1"}})
	if !room.game.Active() {
		t.Fatal("game should become active after every player readies")
	}
	for _, client := range clients {
		if event := receiveEvent(t, client); event.Type != "game_started" {
			t.Fatalf("event = %q, want game_started", event.Type)
		}
		if event := receiveEvent(t, client); event.Type != "role_assigned" {
			t.Fatalf("event = %q, want role_assigned", event.Type)
		}
	}
}

func TestHostCanUpdatePendingGameSettings(t *testing.T) {
	room := newRoom("game-room", nil)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
		testClient(room, "Guest Three", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
	}

	room.handleCommand(roomCommand{client: clients[0], kind: "start_game"})
	for _, client := range clients {
		_ = receiveEvent(t, client)
	}
	room.handleCommand(roomCommand{client: clients[0], kind: "confirm_game_start"})
	for _, client := range clients {
		_ = receiveEvent(t, client)
	}

	settings := game.Settings{Minions: 1, Innocents: 1, Merlins: 1, Assassins: 1}
	room.handleCommand(roomCommand{client: clients[0], kind: "update_game_settings", settings: settings})
	if room.gameSettings != settings || len(room.pendingGameStartConfirmations()) != len(clients) {
		t.Fatalf("pending settings = %#v; pending players = %d", room.gameSettings, len(room.pendingGameStartConfirmations()))
	}
	for _, client := range clients {
		event := receiveEvent(t, client)
		if event.Type != "game_settings_updated" {
			t.Fatalf("event = %q, want game_settings_updated", event.Type)
		}
	}

	for _, readyClient := range clients {
		room.handleCommand(roomCommand{client: readyClient, kind: "confirm_game_start"})
		for _, recipient := range clients {
			_ = receiveEvent(t, recipient)
		}
	}
	room.handleCommand(roomCommand{kind: "launch_game", playerIDs: []string{"2"}})
	counts := map[game.Role]int{}
	for _, role := range room.game.Export().Roles {
		counts[role]++
	}
	if counts[game.Traitor] != 1 || counts[game.Innocent] != 1 || counts[game.Merlin] != 1 || counts[game.Assassin] != 1 {
		t.Fatalf("assigned role counts = %#v", counts)
	}
}

func TestGuestCannotUpdatePendingGameSettings(t *testing.T) {
	room := newRoom("game-room", nil)
	host := testClient(room, "Host", true)
	guest := testClient(room, "Guest", false)
	third := testClient(room, "Third", false)
	for _, client := range []*Client{host, guest, third} {
		room.clients[client] = struct{}{}
	}
	if err := room.prepareGame(host); err != nil {
		t.Fatal(err)
	}
	want := room.gameSettings
	room.handleCommand(roomCommand{
		client: guest, kind: "update_game_settings",
		settings: game.Settings{Minions: 1, Innocents: 2},
	})
	if event := receiveEvent(t, guest); event.Type != "error" {
		t.Fatalf("event = %q, want error", event.Type)
	}
	if room.gameSettings != want {
		t.Fatalf("guest changed settings to %#v, want %#v", room.gameSettings, want)
	}
}

func TestHostCanSaveMismatchedRoleCountButCannotReady(t *testing.T) {
	tests := map[string]game.Settings{
		"more roles":  {Minions: 1, Innocents: 2, Merlins: 1},
		"fewer roles": {Innocents: 1, Assassins: 1},
	}
	for name, settings := range tests {
		t.Run(name, func(t *testing.T) {
			room := newRoom("game-room", nil)
			host := testClient(room, "Host", true)
			guest := testClient(room, "Guest", false)
			third := testClient(room, "Third", false)
			clients := []*Client{host, guest, third}
			for _, client := range clients {
				room.clients[client] = struct{}{}
			}
			if err := room.prepareGame(host); err != nil {
				t.Fatal(err)
			}

			room.handleCommand(roomCommand{client: host, kind: "update_game_settings", settings: settings})
			for _, client := range clients {
				if event := receiveEvent(t, client); event.Type != "game_settings_updated" {
					t.Fatalf("settings event = %q, want game_settings_updated", event.Type)
				}
			}
			if room.gameSettings != settings {
				t.Fatalf("saved settings = %#v, want %#v", room.gameSettings, settings)
			}

			room.handleCommand(roomCommand{client: host, kind: "confirm_game_start"})
			if event := receiveEvent(t, host); event.Type != "error" {
				t.Fatalf("ready event = %q, want error", event.Type)
			}
			if room.gameStartConfirmations[host.participant.ID] {
				t.Fatal("host was readied with a mismatched role count")
			}
		})
	}
}

func TestRoleConfirmationBroadcastsPlayersStillReading(t *testing.T) {
	room := newRoom("game-room", nil)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
	}
	if err := room.startGame(clients[0]); err != nil {
		t.Fatal(err)
	}
	for _, client := range clients {
		_ = receiveEvent(t, client)
		_ = receiveEvent(t, client)
	}

	room.handleCommand(roomCommand{client: clients[0], kind: "confirm_role"})
	for _, client := range clients {
		event := receiveEvent(t, client)
		if event.Type != "role_confirmations_updated" {
			t.Fatalf("event = %q, want role_confirmations_updated", event.Type)
		}
		data := event.Data.(map[string]any)
		pending := data["pendingPlayers"].([]any)
		if len(pending) != 2 {
			t.Fatalf("pending players = %d, want 2", len(pending))
		}
	}
}

func TestGameCommandsBroadcastOnlyPublicProgress(t *testing.T) {
	room := newRoom("game-room", nil)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
		testClient(room, "Guest Three", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
	}
	if err := room.startGame(clients[0]); err != nil {
		t.Fatal(err)
	}
	for _, client := range clients {
		_ = receiveEvent(t, client)
		_ = receiveEvent(t, client)
		room.roleConfirmations[client.participant.ID] = true
	}

	state := room.game.Snapshot()
	var captain *Client
	team := make([]string, 0, state.QuestSize)
	for _, client := range clients {
		if client.participant.ID == state.Captain.ID {
			captain = client
		}
		if len(team) < state.QuestSize {
			team = append(team, client.participant.ID)
		}
	}
	room.handleCommand(roomCommand{client: captain, kind: "propose_quest", playerIDs: team})
	for _, client := range clients {
		event := receiveEvent(t, client)
		if event.Type != "game_updated" {
			t.Fatalf("event = %q, want game_updated", event.Type)
		}
		encoded, _ := json.Marshal(event.Data)
		if string(encoded) == "" || containsAny(string(encoded), "innocent", "traitor", "choice") {
			t.Fatalf("public update leaked private information: %s", encoded)
		}
	}
}

func TestMissedAssassinationContinuesGameAndRevealsAssassin(t *testing.T) {
	room := newRoom("game-room", nil)
	clients := []*Client{
		testClient(room, "Host", true),
		testClient(room, "Guest One", false),
		testClient(room, "Guest Two", false),
		testClient(room, "Guest Three", false),
	}
	for _, client := range clients {
		room.clients[client] = struct{}{}
	}
	if err := room.startGame(clients[0]); err != nil {
		t.Fatal(err)
	}

	var assassin, wrongTarget *Client
	for _, client := range clients {
		_ = receiveEvent(t, client)
		roleEvent := receiveEvent(t, client)
		assignedRole := roleEvent.Data.(map[string]any)["role"].(string)
		if assignedRole == "assassin" {
			assassin = client
		} else if assignedRole == "innocent" {
			wrongTarget = client
		}
	}
	if assassin == nil || wrongTarget == nil {
		t.Fatal("game did not assign both an Assassin and a non-Merlin innocent")
	}

	room.handleCommand(roomCommand{
		client: assassin, kind: "assassinate", playerIDs: []string{wrongTarget.participant.ID},
	})
	for _, client := range clients {
		event := receiveEvent(t, client)
		if event.Type != "game_updated" {
			t.Fatalf("event = %q, want game_updated", event.Type)
		}
		encoded, err := json.Marshal(event.Data)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Active        bool `json:"active"`
			Assassination struct {
				Assassin struct {
					ID string `json:"id"`
				} `json:"assassin"`
				Target struct {
					ID string `json:"id"`
				} `json:"target"`
				Correct bool `json:"correct"`
			} `json:"assassination"`
		}
		if err := json.Unmarshal(encoded, &state); err != nil {
			t.Fatal(err)
		}
		if !state.Active || state.Assassination.Correct || state.Assassination.Assassin.ID != assassin.participant.ID || state.Assassination.Target.ID != wrongTarget.participant.ID {
			t.Fatalf("public missed assassination = %#v", state)
		}
	}

	room.handleCommand(roomCommand{
		client: assassin, kind: "assassinate", playerIDs: []string{wrongTarget.participant.ID},
	})
	if event := receiveEvent(t, assassin); event.Type != "error" {
		t.Fatalf("second assassination event = %q, want error", event.Type)
	}
}

func testClient(room *Room, name string, host bool) *Client {
	return &Client{participant: Participant{ID: name, Name: name, Host: host}, room: room, send: make(chan []byte, 16)}
}

func receiveEvent(t *testing.T, client *Client) Event {
	t.Helper()
	var event Event
	if err := json.Unmarshal(<-client.send, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		for i := 0; i+len(candidate) <= len(value); i++ {
			if value[i:i+len(candidate)] == candidate {
				return true
			}
		}
	}
	return false
}
