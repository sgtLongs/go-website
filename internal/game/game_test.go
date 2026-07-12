package game

import (
	"errors"
	"testing"
)

func TestStartAssignsTraitorAndRandomCaptain(t *testing.T) {
	choices := []int{1, 2}
	engine := newWithChooser(func(int) (int, error) {
		choice := choices[0]
		choices = choices[1:]
		return choice, nil
	})
	started, err := engine.Start(testPlayers())
	if err != nil {
		t.Fatal(err)
	}
	if started.Roles["two"] != Traitor {
		t.Fatalf("chosen role = %q, want %q", started.Roles["two"], Traitor)
	}
	if started.State.Captain.ID != "three" || started.State.Round != 1 || started.State.Phase != ChoosingTeam {
		t.Fatalf("unexpected initial state: %#v", started.State)
	}
	if started.State.QuestSize != 2 {
		t.Fatalf("round 1 quest size = %d, want 2", started.State.QuestSize)
	}
	traitors := 0
	for _, role := range started.Roles {
		if role == Traitor {
			traitors++
		}
	}
	if traitors != 1 {
		t.Fatalf("traitors = %d, want 1", traitors)
	}
}

func TestRejectedProposalRotatesCaptainWithoutAdvancingRound(t *testing.T) {
	engine := startedEngine(t)
	if err := engine.ProposeQuest("one", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if done, err := engine.VoteOnProposal("two", true); err != nil || done {
		t.Fatalf("first vote = %v, %v", done, err)
	}
	if done, err := engine.VoteOnProposal("one", true); err != nil || done {
		t.Fatalf("captain vote = %v, %v", done, err)
	}
	if _, err := engine.VoteOnProposal("two", false); !errors.Is(err, ErrAlreadyVoted) {
		t.Fatalf("duplicate vote error = %v", err)
	}
	if done, err := engine.VoteOnProposal("three", false); err != nil || done {
		t.Fatalf("second vote = %v, %v", done, err)
	}
	if done, err := engine.VoteOnProposal("four", false); err != nil || !done {
		t.Fatalf("final vote = %v, %v", done, err)
	}

	state := engine.Snapshot()
	if state.Phase != ChoosingTeam || state.Captain.ID != "two" || state.Round != 1 {
		t.Fatalf("unexpected state after rejection: %#v", state)
	}
	if state.RejectedProposals != 1 || state.ProposalRejectLimit != ProposalRejectionLimit {
		t.Fatalf("proposal rejection tracker = %d/%d, want 1/%d", state.RejectedProposals, state.ProposalRejectLimit, ProposalRejectionLimit)
	}
	if state.LastProposal == nil || state.LastProposal.Approved || state.LastProposal.Yes != 2 || state.LastProposal.No != 2 {
		t.Fatalf("unexpected proposal result: %#v", state.LastProposal)
	}
}

func TestStrictMajorityApprovesProposal(t *testing.T) {
	engine := startedEngine(t)
	rejectQuest(t, engine)
	if engine.Snapshot().RejectedProposals != 1 {
		t.Fatal("rejected proposal was not tracked")
	}
	state := engine.Snapshot()
	if err := engine.ProposeQuest(state.Captain.ID, questTeam(engine)); err != nil {
		t.Fatal(err)
	}
	_, _ = engine.VoteOnProposal("two", true)
	_, _ = engine.VoteOnProposal("three", true)
	_, _ = engine.VoteOnProposal("four", false)
	done, err := engine.VoteOnProposal("one", true)
	if err != nil || !done {
		t.Fatalf("final vote = %v, %v", done, err)
	}
	if state := engine.Snapshot(); state.Phase != PlayingQuest || state.LastProposal == nil || !state.LastProposal.Approved || state.RejectedProposals != 0 {
		t.Fatalf("unexpected approved state: %#v", state)
	}
}

func TestFiveRejectedProposalsAutomaticallyFailQuestAndResetTracker(t *testing.T) {
	engine := startedEngine(t)
	for rejection := 1; rejection <= ProposalRejectionLimit; rejection++ {
		rejectQuest(t, engine)
		if rejection < ProposalRejectionLimit && engine.Snapshot().RejectedProposals != rejection {
			t.Fatalf("rejection %d was not tracked: %#v", rejection, engine.Snapshot())
		}
	}

	state := engine.Snapshot()
	if state.Round != 2 || state.Phase != ChoosingTeam || state.Captain.ID != "two" {
		t.Fatalf("unexpected state after automatic failure: %#v", state)
	}
	if state.FailedQuests != 1 || state.RejectedProposals != 0 {
		t.Fatalf("failure counts were not updated and reset: %#v", state)
	}
	if state.LastQuest == nil || !state.LastQuest.Automatic || state.LastQuest.Succeeded || len(state.QuestResults) != 1 {
		t.Fatalf("automatic failure was not retained in quest history: %#v", state)
	}
}

func TestQuestRulesAndInnocentVictory(t *testing.T) {
	engine := startedEngine(t)
	for round := 1; round <= 3; round++ {
		captain := engine.Snapshot().Captain.ID
		team := questTeam(engine)
		approveQuest(t, engine, captain, team)
		if _, err := engine.PlayQuestCard("four", true); !errors.Is(err, ErrNotOnQuest) {
			t.Fatalf("non-member card error = %v", err)
		}
		if _, err := engine.PlayQuestCard("two", false); !errors.Is(err, ErrInnocentCannotFail) {
			t.Fatalf("innocent fail error = %v", err)
		}
		for index, playerID := range team {
			resolved, err := engine.PlayQuestCard(playerID, true)
			if err != nil || resolved != (index == len(team)-1) {
				t.Fatalf("card %d resolution = %v, %v", index, resolved, err)
			}
			if index < len(team)-1 {
				submitted := engine.Snapshot().SubmittedQuestPlayers
				if len(submitted) != index+1 || submitted[index] != playerID {
					t.Fatalf("submitted quest players = %#v, want %v through index %d", submitted, team, index)
				}
			}
		}
	}

	state := engine.Snapshot()
	if state.Active || state.Phase != GameComplete || state.Winner != Innocent || state.SuccessfulQuests != 3 {
		t.Fatalf("unexpected completed state: %#v", state)
	}
	if len(state.Traitors) != 1 || state.Traitors[0].ID != "one" {
		t.Fatalf("traitors were not revealed: %#v", state.Traitors)
	}
	if len(state.QuestResults) != 3 {
		t.Fatalf("quest results = %#v, want three results", state.QuestResults)
	}
	for index, result := range state.QuestResults {
		if result.Round != index+1 || !result.Succeeded {
			t.Errorf("quest result %d = %#v, want a success", index+1, result)
		}
	}
}

func TestOneFailCardFailsQuestAndThreeFailuresWin(t *testing.T) {
	engine := startedEngine(t)
	for round := 1; round <= 3; round++ {
		captain := engine.Snapshot().Captain.ID
		team := questTeam(engine)
		approveQuest(t, engine, captain, team)
		for index, playerID := range team {
			resolved, err := engine.PlayQuestCard(playerID, playerID != "one")
			if err != nil || resolved != (index == len(team)-1) {
				t.Fatalf("card %d resolution = %v, %v", index, resolved, err)
			}
		}
		if engine.Snapshot().LastQuest.FailCards != 1 {
			t.Fatalf("fail cards = %d, want 1", engine.Snapshot().LastQuest.FailCards)
		}
	}
	if state := engine.Snapshot(); state.Winner != Traitor || state.FailedQuests != 3 {
		t.Fatalf("unexpected traitor victory: %#v", state)
	} else if len(state.QuestResults) != 3 || state.QuestResults[2].Succeeded {
		t.Fatalf("failed quest history was not retained: %#v", state.QuestResults)
	}
}

func TestStartAndProposalValidation(t *testing.T) {
	engine := newWithChooser(func(int) (int, error) { return 0, nil })
	if _, err := engine.Start(testPlayers()[:2]); !errors.Is(err, ErrNotEnoughPlayers) {
		t.Fatalf("player error = %v", err)
	}
	if _, err := engine.Start(testPlayers()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(testPlayers()); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("active error = %v", err)
	}
	if err := engine.ProposeQuest("two", []string{"one", "two"}); !errors.Is(err, ErrNotCaptain) {
		t.Fatalf("captain error = %v", err)
	}
	for _, team := range [][]string{{"one"}, {"one", "missing"}, {"one", "one"}, {"one", "two", "three"}} {
		if err := engine.ProposeQuest("one", team); !errors.Is(err, ErrInvalidQuest) {
			t.Fatalf("team %v error = %v", team, err)
		}
	}
}

func TestQuestSizesComeFromConfiguredPlayerRanges(t *testing.T) {
	tests := []struct {
		players int
		round   int
		want    int
	}{
		{players: 2, round: 3, want: 1},
		{players: 4, round: 1, want: 2},
		{players: 4, round: 2, want: 3},
		{players: 6, round: 3, want: 4},
		{players: 9, round: 5, want: 5},
		{players: 12, round: 4, want: 6},
	}
	for _, test := range tests {
		if got, ok := QuestSizeFor(test.players, test.round); !ok || got != test.want {
			t.Errorf("QuestSizeFor(%d, %d) = %d, %v; want %d, true", test.players, test.round, got, ok, test.want)
		}
	}
}

func startedEngine(t *testing.T) *Engine {
	t.Helper()
	engine := newWithChooser(func(int) (int, error) { return 0, nil })
	if _, err := engine.Start(testPlayers()); err != nil {
		t.Fatal(err)
	}
	return engine
}

func approveQuest(t *testing.T, engine *Engine, captain string, team []string) {
	t.Helper()
	if err := engine.ProposeQuest(captain, team); err != nil {
		t.Fatal(err)
	}
	for _, player := range testPlayers() {
		if _, err := engine.VoteOnProposal(player.ID, true); err != nil {
			t.Fatal(err)
		}
	}
}

func rejectQuest(t *testing.T, engine *Engine) {
	t.Helper()
	state := engine.Snapshot()
	if err := engine.ProposeQuest(state.Captain.ID, questTeam(engine)); err != nil {
		t.Fatal(err)
	}
	for _, player := range testPlayers() {
		if _, err := engine.VoteOnProposal(player.ID, false); err != nil {
			t.Fatal(err)
		}
	}
}

func questTeam(engine *Engine) []string {
	state := engine.Snapshot()
	team := make([]string, state.QuestSize)
	for index := range team {
		team[index] = state.Players[index].ID
	}
	return team
}

func testPlayers() []Player {
	return []Player{
		{ID: "one", Name: "One"},
		{ID: "two", Name: "Two"},
		{ID: "three", Name: "Three"},
		{ID: "four", Name: "Four"},
	}
}
