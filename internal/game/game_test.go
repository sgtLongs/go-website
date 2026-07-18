package game

import (
	"errors"
	"reflect"
	"testing"
)

func TestPersistedStateRestoresPrivateProgress(t *testing.T) {
	original := newWithChooser(func(int) (int, error) { return 0, nil })
	started, err := original.Start(testPlayers())
	if err != nil {
		t.Fatal(err)
	}
	team := make([]string, started.State.QuestSize)
	for i := range team {
		team[i] = started.State.Players[i].ID
	}
	if err := original.ProposeQuest(started.State.Captain.ID, team); err != nil {
		t.Fatal(err)
	}
	voter := started.State.Players[0].ID
	if _, err := original.VoteOnProposal(voter, false); err != nil {
		t.Fatal(err)
	}

	restored := New()
	if err := restored.Restore(original.Export()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Snapshot(), original.Snapshot()) {
		t.Fatalf("restored public snapshot = %#v, want %#v", restored.Snapshot(), original.Snapshot())
	}
	if !restored.HasVoted(voter) {
		t.Fatal("restored engine lost the player's submitted vote")
	}
	for playerID, want := range started.Roles {
		if got, ok := restored.RoleFor(playerID); !ok || got != want {
			t.Fatalf("restored role for %q = %q, %v; want %q, true", playerID, got, ok, want)
		}
	}
}

func TestRestoreRejectsTamperedRole(t *testing.T) {
	engine := newWithChooser(func(int) (int, error) { return 0, nil })
	if _, err := engine.Start(testPlayers()); err != nil {
		t.Fatal(err)
	}
	state := engine.Export()
	state.Roles[state.Players[0].ID] = "administrator"
	if err := New().Restore(state); err == nil {
		t.Fatal("Restore accepted an invalid role")
	}
}

func TestRestoreUpgradesLegacyFactionRoles(t *testing.T) {
	engine := startedEngine(t)
	state := engine.Export()
	state.Settings = Settings{}
	for id, role := range state.Roles {
		switch role {
		case Assassin:
			state.Roles[id] = Traitor
		case Merlin:
			state.Roles[id] = Innocent
		}
	}

	restored := New()
	if err := restored.Restore(state); err != nil {
		t.Fatal(err)
	}
	roles := restored.Export().Roles
	if roles["one"] != Assassin || roles["two"] != Merlin {
		t.Fatalf("upgraded roles = %#v", roles)
	}
}

func TestStartAssignsMerlinAssassinAndRandomCaptain(t *testing.T) {
	choices := []int{1, 0, 2}
	engine := newWithChooser(func(int) (int, error) {
		choice := choices[0]
		choices = choices[1:]
		return choice, nil
	})
	started, err := engine.Start(testPlayers())
	if err != nil {
		t.Fatal(err)
	}
	if started.Roles["two"] != Assassin {
		t.Fatalf("chosen traitor role = %q, want %q", started.Roles["two"], Assassin)
	}
	if started.Roles["one"] != Merlin {
		t.Fatalf("chosen innocent role = %q, want %q", started.Roles["one"], Merlin)
	}
	if started.State.Captain.ID != "three" || started.State.Round != 1 || started.State.Phase != ChoosingTeam {
		t.Fatalf("unexpected initial state: %#v", started.State)
	}
	if started.State.QuestSize != 2 {
		t.Fatalf("round 1 quest size = %d, want 2", started.State.QuestSize)
	}
	traitors, merlins, assassins := 0, 0, 0
	for _, role := range started.Roles {
		if isTraitor(role) {
			traitors++
		}
		if role == Merlin {
			merlins++
		}
		if role == Assassin {
			assassins++
		}
	}
	if traitors != 1 || merlins != 1 || assassins != 1 {
		t.Fatalf("special role counts = traitors %d, Merlins %d, Assassins %d; want 1 each", traitors, merlins, assassins)
	}
}

func TestStartWithSettingsAssignsRequestedRoleCounts(t *testing.T) {
	engine := newWithChooser(func(int) (int, error) { return 0, nil })
	settings := Settings{Minions: 1, Innocents: 1, Merlins: 1, Assassins: 1}
	started, err := engine.StartWithSettings(testPlayers(), settings)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Role{
		"one":   Assassin,
		"two":   Traitor,
		"three": Merlin,
		"four":  Innocent,
	}
	if !reflect.DeepEqual(started.Roles, want) {
		t.Fatalf("roles = %#v, want %#v", started.Roles, want)
	}
	if engine.Export().Settings != settings || len(engine.Export().Traitors) != 2 {
		t.Fatalf("started state = %#v", started.State)
	}

	restored := New()
	if err := restored.Restore(engine.Export()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Export().Roles, want) {
		t.Fatalf("restored roles = %#v, want %#v", restored.Export().Roles, want)
	}
}

func TestSettingsValidation(t *testing.T) {
	valid := Settings{Minions: 1, Innocents: 1, Merlins: 1, Assassins: 1}
	if err := valid.Validate(4); err != nil {
		t.Fatalf("valid settings error = %v", err)
	}
	if err := (Settings{Innocents: 1, Assassins: 1}).ValidateComposition(); err != nil {
		t.Fatalf("mismatched pending settings error = %v", err)
	}
	invalid := []Settings{
		{Minions: -1, Innocents: 3, Merlins: 1, Assassins: 1},
		{Innocents: 1, Merlins: 2, Assassins: 1},
		{Innocents: 1, Merlins: 1, Assassins: 2},
		{Minions: 1, Innocents: 1},
		{Innocents: 4},
		{Minions: 4},
	}
	for _, settings := range invalid {
		if err := settings.Validate(4); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("Validate(%#v) error = %v, want ErrInvalidSettings", settings, err)
		}
	}
}

func TestQuestSettingsValidation(t *testing.T) {
	valid := Settings{
		Minions: 2, Innocents: 2,
		QuestSizes:          [TotalRounds]int{3, 2, 4, 3, 2},
		QuestFailThresholds: [TotalRounds]int{2, 1, 3, 2, 1},
	}
	if err := valid.Validate(4); err != nil {
		t.Fatalf("valid quest settings error = %v", err)
	}

	invalid := []Settings{
		{Minions: 2, Innocents: 2, QuestSizes: [TotalRounds]int{5, 2, 2, 2, 2}},
		{Minions: 2, Innocents: 2, QuestSizes: [TotalRounds]int{2, 2, 2, 2, 2}, QuestFailThresholds: [TotalRounds]int{3, 1, 1, 1, 1}},
		{Minions: 2, Innocents: 2, QuestSizes: [TotalRounds]int{-1, 2, 2, 2, 2}},
	}
	for _, settings := range invalid {
		if err := settings.Validate(4); !errors.Is(err, ErrInvalidQuestRules) {
			t.Errorf("Validate(%#v) error = %v, want ErrInvalidQuestRules", settings, err)
		}
	}
}

func TestDefaultQuestSettingsUsesConfiguredRules(t *testing.T) {
	settings := DefaultQuestSettings(7)
	want := [TotalRounds]int{2, 3, 3, 4, 4}
	if settings.QuestSizes != want {
		t.Fatalf("default quest sizes = %v, want %v", settings.QuestSizes, want)
	}
	if settings.QuestFailThresholds != [TotalRounds]int{1, 1, 1, 2, 1} {
		t.Fatalf("default quest failure thresholds = %v, want round four to require two", settings.QuestFailThresholds)
	}
}

func TestDefaultSettingsUsesRecommendedRoles(t *testing.T) {
	want := Settings{RecommendedSettings: true, Minions: 2, Innocents: 4, Merlins: 1, Assassins: 1}
	if got := DefaultSettings(8); got != want {
		t.Fatalf("default settings = %#v, want %#v", got, want)
	}
}

func TestCustomQuestSizeAndFailureThreshold(t *testing.T) {
	settings := Settings{
		Minions: 2, Innocents: 2,
		QuestSizes:          [TotalRounds]int{3, 3, 3, 3, 3},
		QuestFailThresholds: [TotalRounds]int{2, 2, 2, 2, 2},
	}
	playFirstQuest := func(t *testing.T, cards map[string]bool) Snapshot {
		t.Helper()
		engine := newWithChooser(func(int) (int, error) { return 0, nil })
		started, err := engine.StartWithSettings(testPlayers(), settings)
		if err != nil {
			t.Fatal(err)
		}
		if started.State.QuestSize != 3 || started.State.QuestFailsNeeded != 2 {
			t.Fatalf("quest rule = %d players/%d failures, want 3/2", started.State.QuestSize, started.State.QuestFailsNeeded)
		}
		team := []string{"one", "two", "three"}
		approveQuest(t, engine, started.State.Captain.ID, team)
		for _, playerID := range team {
			if _, err := engine.PlayQuestCard(playerID, cards[playerID]); err != nil {
				t.Fatal(err)
			}
		}
		return engine.Snapshot()
	}

	succeeded := playFirstQuest(t, map[string]bool{"one": false, "two": true, "three": true})
	if succeeded.LastQuest == nil || !succeeded.LastQuest.Succeeded || succeeded.LastQuest.FailCards != 1 || succeeded.LastQuest.FailsNeeded != 2 {
		t.Fatalf("one-failure quest result = %#v, want success with two failures needed", succeeded.LastQuest)
	}

	failed := playFirstQuest(t, map[string]bool{"one": false, "two": false, "three": true})
	if failed.LastQuest == nil || failed.LastQuest.Succeeded || failed.LastQuest.FailCards != 2 || failed.LastQuest.FailsNeeded != 2 {
		t.Fatalf("two-failure quest result = %#v, want failure", failed.LastQuest)
	}
}

func TestRestorePreservesCustomRolesWithoutSpecialCharacters(t *testing.T) {
	engine := newWithChooser(func(int) (int, error) { return 0, nil })
	settings := Settings{Minions: 2, Innocents: 2}
	if _, err := engine.StartWithSettings(testPlayers(), settings); err != nil {
		t.Fatal(err)
	}
	restored := New()
	if err := restored.Restore(engine.Export()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Export().Roles, engine.Export().Roles) {
		t.Fatalf("restored roles = %#v, want %#v", restored.Export().Roles, engine.Export().Roles)
	}
}

func TestRestoreUpgradesMissedAssassinationToDeadState(t *testing.T) {
	engine := startedEngine(t)
	if _, err := engine.Assassinate("one", "three"); err != nil {
		t.Fatal(err)
	}
	state := engine.Export()
	state.Players[2].Dead = false
	state.Assassination.Target.Dead = false

	restored := New()
	if err := restored.Restore(state); err != nil {
		t.Fatal(err)
	}
	if !restored.IsDead("three") || !restored.Snapshot().Assassination.Target.Dead {
		t.Fatalf("legacy assassination was not upgraded: %#v", restored.Snapshot())
	}
}

func TestMerlinKnowledgeAndMissedAssassination(t *testing.T) {
	engine := startedEngine(t)
	if got := engine.KnownRolesFor("two"); !reflect.DeepEqual(got, map[string]Role{"one": Traitor, "two": Merlin}) {
		t.Fatalf("Merlin knowledge = %#v", got)
	}
	if got := engine.KnownRolesFor("one"); !reflect.DeepEqual(got, map[string]Role{"one": Assassin}) {
		t.Fatalf("Assassin knowledge = %#v", got)
	}
	if _, err := engine.Assassinate("three", "two"); !errors.Is(err, ErrNotAssassin) {
		t.Fatalf("non-Assassin error = %v", err)
	}
	if _, err := engine.Assassinate("one", "one"); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("self-target error = %v", err)
	}

	before := engine.Snapshot()
	correct, err := engine.Assassinate("one", "three")
	if err != nil || correct {
		t.Fatalf("missed assassination = %v, %v", correct, err)
	}
	state := engine.Snapshot()
	if !state.Active || state.Phase != before.Phase || state.Round != before.Round {
		t.Fatalf("missed assassination interrupted play: %#v", state)
	}
	if state.Assassination == nil || state.Assassination.Assassin.ID != "one" || state.Assassination.Target.ID != "three" || state.Assassination.Correct {
		t.Fatalf("public assassination = %#v", state.Assassination)
	}
	if !state.Players[2].Dead || !state.Assassination.Target.Dead || !engine.IsDead("three") {
		t.Fatalf("assassination target was not marked dead: %#v", state)
	}
	if _, err := engine.Assassinate("one", "two"); !errors.Is(err, ErrAssassinationUsed) {
		t.Fatalf("second attempt error = %v", err)
	}

	restored := New()
	if err := restored.Restore(engine.Export()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Snapshot(), state) {
		t.Fatalf("restored assassination = %#v, want %#v", restored.Snapshot(), state)
	}
}

func TestDeadPlayerCannotJoinQuestsOrVote(t *testing.T) {
	engine := startedEngine(t)
	if err := engine.ProposeQuest("one", []string{"one", "three"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.VoteOnProposal("one", true); err != nil {
		t.Fatal(err)
	}
	if correct, err := engine.Assassinate("one", "three"); err != nil || correct {
		t.Fatalf("missed assassination = %v, %v", correct, err)
	}

	state := engine.Snapshot()
	if state.Phase != ChoosingTeam || len(state.Quest) != 0 || state.ProposalVotesCast != 0 || state.ProposalVotesNeeded != 3 {
		t.Fatalf("missed assassination did not reset team selection for living players: %#v", state)
	}
	if err := engine.ProposeQuest("one", []string{"one", "three"}); !errors.Is(err, ErrInvalidQuest) {
		t.Fatalf("dead quest member error = %v, want ErrInvalidQuest", err)
	}
	if err := engine.ProposeQuest("one", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.VoteOnProposal("three", true); !errors.Is(err, ErrDeadPlayer) {
		t.Fatalf("dead voter error = %v, want ErrDeadPlayer", err)
	}
	for _, id := range []string{"one", "two", "four"} {
		if _, err := engine.VoteOnProposal(id, true); err != nil {
			t.Fatal(err)
		}
	}
	if engine.Snapshot().Phase != PlayingQuest {
		t.Fatalf("living-player vote did not complete: %#v", engine.Snapshot())
	}
}

func TestMissedAssassinationSkipsDeadCaptain(t *testing.T) {
	choices := []int{0, 0, 2}
	engine := newWithChooser(func(int) (int, error) {
		choice := choices[0]
		choices = choices[1:]
		return choice, nil
	})
	if _, err := engine.Start(testPlayers()); err != nil {
		t.Fatal(err)
	}
	if engine.Snapshot().Captain.ID != "three" {
		t.Fatalf("initial captain = %q, want three", engine.Snapshot().Captain.ID)
	}
	if _, err := engine.Assassinate("one", "three"); err != nil {
		t.Fatal(err)
	}
	if captain := engine.Snapshot().Captain; captain.ID != "four" || captain.Dead {
		t.Fatalf("captain after assassination = %#v, want living player four", captain)
	}
}

func TestQuestSizeUsesLivingPlayerCount(t *testing.T) {
	engine := newWithChooser(func(int) (int, error) { return 0, nil })
	settings := Settings{Minions: 1, Innocents: 1, Assassins: 1}
	if _, err := engine.StartWithSettings(testPlayers()[:3], settings); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Assassinate("one", "three"); err != nil {
		t.Fatal(err)
	}
	if size := engine.Snapshot().QuestSize; size != 1 {
		t.Fatalf("quest size after death = %d, want 1 for two living players", size)
	}
}

func TestCorrectAssassinationGivesTraitorsVictory(t *testing.T) {
	engine := startedEngine(t)
	correct, err := engine.Assassinate("one", "two")
	if err != nil || !correct {
		t.Fatalf("correct assassination = %v, %v", correct, err)
	}
	state := engine.Snapshot()
	if state.Active || state.Phase != GameComplete || state.Winner != Traitor {
		t.Fatalf("correct assassination did not finish the game: %#v", state)
	}
	if state.Assassination == nil || !state.Assassination.Correct {
		t.Fatalf("correct assassination was not published: %#v", state.Assassination)
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
