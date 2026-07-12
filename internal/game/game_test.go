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
	if err := engine.ProposeQuest("one", []string{"one", "two", "three"}); err != nil {
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
	if state.LastProposal == nil || state.LastProposal.Approved || state.LastProposal.Yes != 2 || state.LastProposal.No != 2 {
		t.Fatalf("unexpected proposal result: %#v", state.LastProposal)
	}
}

func TestStrictMajorityApprovesProposal(t *testing.T) {
	engine := startedEngine(t)
	if err := engine.ProposeQuest("one", []string{"one", "two", "three"}); err != nil {
		t.Fatal(err)
	}
	_, _ = engine.VoteOnProposal("two", true)
	_, _ = engine.VoteOnProposal("three", true)
	_, _ = engine.VoteOnProposal("four", false)
	done, err := engine.VoteOnProposal("one", true)
	if err != nil || !done {
		t.Fatalf("final vote = %v, %v", done, err)
	}
	if state := engine.Snapshot(); state.Phase != PlayingQuest || state.LastProposal == nil || !state.LastProposal.Approved {
		t.Fatalf("unexpected approved state: %#v", state)
	}
}

func TestQuestRulesAndInnocentVictory(t *testing.T) {
	engine := startedEngine(t)
	for round := 1; round <= 3; round++ {
		captain := engine.Snapshot().Captain.ID
		approveQuest(t, engine, captain, []string{"one", "two", "three"})
		if _, err := engine.PlayQuestCard("four", true); !errors.Is(err, ErrNotOnQuest) {
			t.Fatalf("non-member card error = %v", err)
		}
		if _, err := engine.PlayQuestCard("two", false); !errors.Is(err, ErrInnocentCannotFail) {
			t.Fatalf("innocent fail error = %v", err)
		}
		_, _ = engine.PlayQuestCard("one", true)
		_, _ = engine.PlayQuestCard("two", true)
		resolved, err := engine.PlayQuestCard("three", true)
		if err != nil || !resolved {
			t.Fatalf("quest resolution = %v, %v", resolved, err)
		}
	}

	state := engine.Snapshot()
	if state.Active || state.Phase != GameComplete || state.Winner != Innocent || state.SuccessfulQuests != 3 {
		t.Fatalf("unexpected completed state: %#v", state)
	}
	if len(state.Traitors) != 1 || state.Traitors[0].ID != "one" {
		t.Fatalf("traitors were not revealed: %#v", state.Traitors)
	}
}

func TestOneFailCardFailsQuestAndThreeFailuresWin(t *testing.T) {
	engine := startedEngine(t)
	for round := 1; round <= 3; round++ {
		captain := engine.Snapshot().Captain.ID
		approveQuest(t, engine, captain, []string{"one", "two", "three"})
		_, _ = engine.PlayQuestCard("one", false)
		_, _ = engine.PlayQuestCard("two", true)
		resolved, err := engine.PlayQuestCard("three", true)
		if err != nil || !resolved {
			t.Fatalf("quest resolution = %v, %v", resolved, err)
		}
		if engine.Snapshot().LastQuest.FailCards != 1 {
			t.Fatalf("fail cards = %d, want 1", engine.Snapshot().LastQuest.FailCards)
		}
	}
	if state := engine.Snapshot(); state.Winner != Traitor || state.FailedQuests != 3 {
		t.Fatalf("unexpected traitor victory: %#v", state)
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
	if err := engine.ProposeQuest("two", []string{"one", "two", "three"}); !errors.Is(err, ErrNotCaptain) {
		t.Fatalf("captain error = %v", err)
	}
	for _, team := range [][]string{{"one", "two"}, {"one", "two", "missing"}, {"one", "two", "two"}} {
		if err := engine.ProposeQuest("one", team); !errors.Is(err, ErrInvalidQuest) {
			t.Fatalf("team %v error = %v", team, err)
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

func testPlayers() []Player {
	return []Player{
		{ID: "one", Name: "One"},
		{ID: "two", Name: "Two"},
		{ID: "three", Name: "Three"},
		{ID: "four", Name: "Four"},
	}
}
