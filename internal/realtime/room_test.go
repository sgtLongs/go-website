package realtime

import (
	"encoding/json"
	"testing"
)

func TestHostStartsGameBroadcastsStateAndAssignments(t *testing.T) {
	room := newRoom("game-room")
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

	traitors := 0
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
		if data["role"] == "traitor" {
			traitors++
		}
	}
	if traitors != 1 {
		t.Fatalf("traitor roles = %d, want 1", traitors)
	}
}

func TestOnlyHostCanStartGame(t *testing.T) {
	room := newRoom("game-room")
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
	room := newRoom("game-room")
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
		if i < len(clients)-1 {
			if room.game.Active() {
				t.Fatalf("game became active after only %d ready players", i+1)
			}
			for _, recipient := range clients {
				if event := receiveEvent(t, recipient); event.Type != "game_start_confirmations_updated" {
					t.Fatalf("event = %q, want game_start_confirmations_updated", event.Type)
				}
			}
		}
	}
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

func TestRoleConfirmationBroadcastsPlayersStillReading(t *testing.T) {
	room := newRoom("game-room")
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
	room := newRoom("game-room")
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
