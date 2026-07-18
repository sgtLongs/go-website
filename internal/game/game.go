// Package game contains the social-deduction rules and lifecycle. It knows
// nothing about WebSockets, rooms, or browser clients.
package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

const (
	MinPlayers             = 3
	TotalRounds            = 5
	WinningQuests          = 3
	ProposalRejectionLimit = 5
)

var (
	ErrAlreadyActive      = errors.New("a game is already running")
	ErrNotEnoughPlayers   = errors.New("at least three players are needed")
	ErrInvalidSettings    = errors.New("role counts must match the player count and include both factions")
	ErrNotActive          = errors.New("no game is running")
	ErrWrongPhase         = errors.New("that action is not allowed right now")
	ErrNotCaptain         = errors.New("only the captain can choose the quest team")
	ErrInvalidQuest       = errors.New("choose the required number of different players")
	ErrDeadPlayer         = errors.New("dead players cannot take part in the game")
	ErrMissingQuestRule   = errors.New("no quest rule is configured for these players")
	ErrNotProposalVoter   = errors.New("only players in this game may vote on the proposal")
	ErrAlreadyVoted       = errors.New("you have already voted")
	ErrNotOnQuest         = errors.New("you are not on this quest")
	ErrInnocentCannotFail = errors.New("innocent players cannot fail a quest")
	ErrNotAssassin        = errors.New("only the assassin may assassinate a player")
	ErrAssassinationUsed  = errors.New("the assassin has already made an attempt")
	ErrInvalidTarget      = errors.New("choose another player to assassinate")
)

type Role string

const (
	Innocent Role = "innocent"
	Traitor  Role = "traitor"
	Merlin   Role = "merlin"
	Assassin Role = "assassin"
)

// Settings controls how many players receive each role. Minions and
// Assassins belong to the traitor faction; Innocents and Merlins belong to the
// innocent faction.
type Settings struct {
	Minions   int `json:"minions"`
	Innocents int `json:"innocents"`
	Merlins   int `json:"merlins"`
	Assassins int `json:"assassins"`
}

func (settings Settings) Total() int {
	return settings.Minions + settings.Innocents + settings.Merlins + settings.Assassins
}

func DefaultSettings(playerCount int) Settings {
	if playerCount < 2 {
		return Settings{Innocents: playerCount}
	}
	return Settings{Innocents: playerCount - 2, Merlins: 1, Assassins: 1}
}

func (settings Settings) Validate(playerCount int) error {
	if err := settings.ValidateComposition(); err != nil {
		return err
	}
	if settings.Total() != playerCount {
		return ErrInvalidSettings
	}
	return nil
}

func (settings Settings) ValidateComposition() error {
	if settings.Minions < 0 || settings.Innocents < 0 || settings.Merlins < 0 || settings.Assassins < 0 {
		return ErrInvalidSettings
	}
	if settings.Merlins > 1 || settings.Assassins > 1 {
		return ErrInvalidSettings
	}
	if settings.Minions+settings.Assassins == 0 || settings.Innocents+settings.Merlins == 0 {
		return ErrInvalidSettings
	}
	return nil
}

type Phase string

const (
	ChoosingTeam Phase = "choosing_team"
	VotingOnTeam Phase = "voting_on_team"
	PlayingQuest Phase = "playing_quest"
	GameComplete Phase = "complete"
)

type Player struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dead bool   `json:"dead,omitempty"`
}

type ProposalResult struct {
	Approved bool `json:"approved"`
	Yes      int  `json:"yes"`
	No       int  `json:"no"`
}

type QuestResult struct {
	Round        int  `json:"round"`
	Succeeded    bool `json:"succeeded"`
	SuccessCards int  `json:"successCards"`
	FailCards    int  `json:"failCards"`
	Automatic    bool `json:"automatic,omitempty"`
}

type AssassinationResult struct {
	Assassin Player `json:"assassin"`
	Target   Player `json:"target"`
	Correct  bool   `json:"correct"`
}

// Snapshot is public game information. It deliberately contains no secret
// roles or individual votes. An Assassin is included only after their public
// assassination attempt, so the snapshot is safe to broadcast to everyone.
type Snapshot struct {
	Active                bool                 `json:"active"`
	Phase                 Phase                `json:"phase"`
	Round                 int                  `json:"round"`
	TotalRounds           int                  `json:"totalRounds"`
	Players               []Player             `json:"players"`
	Captain               Player               `json:"captain"`
	Quest                 []Player             `json:"quest,omitempty"`
	QuestSize             int                  `json:"questSize"`
	QuestSizes            []int                `json:"questSizes"`
	ProposalVotesCast     int                  `json:"proposalVotesCast"`
	ProposalVotesNeeded   int                  `json:"proposalVotesNeeded"`
	RejectedProposals     int                  `json:"rejectedProposals"`
	ProposalRejectLimit   int                  `json:"proposalRejectLimit"`
	QuestCardsPlayed      int                  `json:"questCardsPlayed"`
	QuestCardsNeeded      int                  `json:"questCardsNeeded"`
	SubmittedQuestPlayers []string             `json:"submittedQuestPlayers,omitempty"`
	SuccessfulQuests      int                  `json:"successfulQuests"`
	FailedQuests          int                  `json:"failedQuests"`
	QuestResults          []QuestResult        `json:"questResults"`
	LastProposal          *ProposalResult      `json:"lastProposal,omitempty"`
	LastQuest             *QuestResult         `json:"lastQuest,omitempty"`
	Winner                Role                 `json:"winner,omitempty"`
	Traitors              []Player             `json:"traitors,omitempty"`
	Assassination         *AssassinationResult `json:"assassination,omitempty"`
}

type Started struct {
	Generation uint64
	Roles      map[string]Role
	State      Snapshot
}

// PersistedState contains every durable rule-engine field, including private
// roles and choices. It is intended only for trusted server-side storage.
type PersistedState struct {
	Active        bool                 `json:"active"`
	Generation    uint64               `json:"generation"`
	Players       []Player             `json:"players"`
	Roles         map[string]Role      `json:"roles"`
	CaptainIndex  int                  `json:"captainIndex"`
	Phase         Phase                `json:"phase"`
	Round         int                  `json:"round"`
	Quest         []Player             `json:"quest,omitempty"`
	ProposalVotes map[string]bool      `json:"proposalVotes,omitempty"`
	RejectedTeams int                  `json:"rejectedTeams"`
	QuestCards    map[string]bool      `json:"questCards,omitempty"`
	Successful    int                  `json:"successful"`
	Failed        int                  `json:"failed"`
	LastProposal  *ProposalResult      `json:"lastProposal,omitempty"`
	LastQuest     *QuestResult         `json:"lastQuest,omitempty"`
	QuestResults  []QuestResult        `json:"questResults,omitempty"`
	Winner        Role                 `json:"winner,omitempty"`
	Traitors      []Player             `json:"traitors,omitempty"`
	Assassination *AssassinationResult `json:"assassination,omitempty"`
	Settings      Settings             `json:"settings"`
}

type Engine struct {
	active        bool
	generation    uint64
	players       []Player
	playerByID    map[string]Player
	roles         map[string]Role
	captainIndex  int
	phase         Phase
	round         int
	quest         []Player
	proposalVotes map[string]bool
	rejectedTeams int
	questCards    map[string]bool
	successful    int
	failed        int
	lastProposal  *ProposalResult
	lastQuest     *QuestResult
	questResults  []QuestResult
	winner        Role
	traitors      []Player
	assassination *AssassinationResult
	settings      Settings
	choose        func(int) (int, error)
}

func New() *Engine {
	return newWithChooser(func(total int) (int, error) {
		choice, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
		if err != nil {
			return 0, err
		}
		return int(choice.Int64()), nil
	})
}

func newWithChooser(choose func(int) (int, error)) *Engine {
	return &Engine{choose: choose}
}

func (g *Engine) Start(players []Player) (Started, error) {
	return g.StartWithSettings(players, DefaultSettings(len(players)))
}

func (g *Engine) StartWithSettings(players []Player, settings Settings) (Started, error) {
	if g.active {
		return Started{}, ErrAlreadyActive
	}
	if len(players) < MinPlayers {
		return Started{}, ErrNotEnoughPlayers
	}
	if err := settings.Validate(len(players)); err != nil {
		return Started{}, err
	}
	for round := 1; round <= TotalRounds; round++ {
		if _, exists := QuestSizeFor(len(players), round); !exists {
			return Started{}, ErrMissingQuestRule
		}
	}
	players = append([]Player(nil), players...)

	roles := make(map[string]Role, len(players))
	playerByID := make(map[string]Player, len(players))
	for index, player := range players {
		player.Dead = false
		players[index] = player
		roles[player.ID] = Innocent
		playerByID[player.ID] = player
	}
	remaining := append([]Player(nil), players...)
	assign := func(role Role, count int) error {
		for range count {
			index, err := g.randomIndex(len(remaining))
			if err != nil {
				return err
			}
			roles[remaining[index].ID] = role
			remaining = append(remaining[:index], remaining[index+1:]...)
		}
		return nil
	}
	if err := assign(Assassin, settings.Assassins); err != nil {
		return Started{}, err
	}
	if err := assign(Traitor, settings.Minions); err != nil {
		return Started{}, err
	}
	if err := assign(Merlin, settings.Merlins); err != nil {
		return Started{}, err
	}
	captainIndex, err := g.randomIndex(len(players))
	if err != nil {
		return Started{}, err
	}
	traitors := make([]Player, 0, settings.Minions+settings.Assassins)
	for _, player := range players {
		if isTraitor(roles[player.ID]) {
			traitors = append(traitors, player)
		}
	}

	g.generation++
	g.active = true
	g.players = append([]Player(nil), players...)
	g.playerByID = playerByID
	g.roles = roles
	g.captainIndex = captainIndex
	g.phase = ChoosingTeam
	g.round = 1
	g.quest = nil
	g.proposalVotes = nil
	g.rejectedTeams = 0
	g.questCards = nil
	g.successful = 0
	g.failed = 0
	g.lastProposal = nil
	g.lastQuest = nil
	g.questResults = nil
	g.winner = ""
	g.traitors = traitors
	g.assassination = nil
	g.settings = settings

	return Started{Generation: g.generation, Roles: copyRoles(roles), State: g.Snapshot()}, nil
}

func (g *Engine) ProposeQuest(captainID string, playerIDs []string) error {
	if !g.active {
		return ErrNotActive
	}
	if g.phase != ChoosingTeam {
		return ErrWrongPhase
	}
	if g.players[g.captainIndex].ID != captainID {
		return ErrNotCaptain
	}
	if g.isDead(captainID) {
		return ErrDeadPlayer
	}
	questSize := g.currentQuestSize()
	if len(playerIDs) != questSize {
		return ErrInvalidQuest
	}

	seen := make(map[string]struct{}, questSize)
	quest := make([]Player, 0, questSize)
	for _, id := range playerIDs {
		player, exists := g.playerByID[id]
		if _, duplicate := seen[id]; !exists || duplicate || player.Dead {
			return ErrInvalidQuest
		}
		seen[id] = struct{}{}
		quest = append(quest, player)
	}

	g.quest = quest
	g.proposalVotes = make(map[string]bool, g.livingPlayerCount())
	g.phase = VotingOnTeam
	g.lastProposal = nil
	g.lastQuest = nil
	return nil
}

// VoteOnProposal returns true when the final required vote was submitted.
func (g *Engine) VoteOnProposal(playerID string, approve bool) (bool, error) {
	if !g.active {
		return false, ErrNotActive
	}
	if g.phase != VotingOnTeam {
		return false, ErrWrongPhase
	}
	if _, exists := g.playerByID[playerID]; !exists {
		return false, ErrNotProposalVoter
	}
	if g.isDead(playerID) {
		return false, ErrDeadPlayer
	}
	if _, voted := g.proposalVotes[playerID]; voted {
		return false, ErrAlreadyVoted
	}

	g.proposalVotes[playerID] = approve
	if len(g.proposalVotes) < g.livingPlayerCount() {
		return false, nil
	}

	yes := 0
	for _, vote := range g.proposalVotes {
		if vote {
			yes++
		}
	}
	no := len(g.proposalVotes) - yes
	approved := yes > len(g.proposalVotes)/2
	g.lastProposal = &ProposalResult{Approved: approved, Yes: yes, No: no}
	if approved {
		g.rejectedTeams = 0
		g.phase = PlayingQuest
		g.questCards = make(map[string]bool, len(g.quest))
	} else {
		g.rejectedTeams++
		if g.rejectedTeams == ProposalRejectionLimit {
			g.rejectedTeams = 0
			g.resolveQuest(QuestResult{Round: g.round, Automatic: true})
			return true, nil
		}
		g.advanceCaptain()
		g.phase = ChoosingTeam
		g.quest = nil
		g.proposalVotes = nil
	}
	return true, nil
}

// PlayQuestCard records true for success and false for failure. It returns
// true when the quest was resolved by this card.
func (g *Engine) PlayQuestCard(playerID string, succeed bool) (bool, error) {
	if !g.active {
		return false, ErrNotActive
	}
	if g.phase != PlayingQuest {
		return false, ErrWrongPhase
	}
	if g.isDead(playerID) {
		return false, ErrDeadPlayer
	}
	if !g.onQuest(playerID) {
		return false, ErrNotOnQuest
	}
	if _, played := g.questCards[playerID]; played {
		return false, ErrAlreadyVoted
	}
	if !succeed && !isTraitor(g.roles[playerID]) {
		return false, ErrInnocentCannotFail
	}

	g.questCards[playerID] = succeed
	if len(g.questCards) < len(g.quest) {
		return false, nil
	}

	successCards := 0
	for _, card := range g.questCards {
		if card {
			successCards++
		}
	}
	failCards := len(g.quest) - successCards
	succeeded := failCards == 0
	result := QuestResult{
		Round: g.round, Succeeded: succeeded,
		SuccessCards: successCards, FailCards: failCards,
	}
	g.resolveQuest(result)
	return true, nil
}

// Assassinate gives the Assassin one attempt during an active game. A correct
// guess ends the game in a traitor victory; an incorrect guess reveals the
// Assassin publicly, kills the target, and returns the round to team selection.
func (g *Engine) Assassinate(assassinID, targetID string) (bool, error) {
	if !g.active {
		return false, ErrNotActive
	}
	if g.roles[assassinID] != Assassin {
		return false, ErrNotAssassin
	}
	if g.assassination != nil {
		return false, ErrAssassinationUsed
	}
	_, exists := g.playerByID[targetID]
	if !exists || targetID == assassinID {
		return false, ErrInvalidTarget
	}

	correct := g.roles[targetID] == Merlin
	g.markDead(targetID)
	g.assassination = &AssassinationResult{
		Assassin: g.playerByID[assassinID],
		Target:   g.playerByID[targetID],
		Correct:  correct,
	}
	if correct {
		g.finish(Traitor)
	} else {
		g.phase = ChoosingTeam
		g.quest = nil
		g.proposalVotes = nil
		g.questCards = nil
		g.lastProposal = nil
		if g.players[g.captainIndex].Dead {
			g.advanceCaptain()
		}
	}
	return correct, nil
}

func (g *Engine) Active() bool { return g.active }

func (g *Engine) HasPlayer(playerID string) bool {
	_, exists := g.playerByID[playerID]
	return exists
}

func (g *Engine) IsDead(playerID string) bool {
	return g.isDead(playerID)
}

func (g *Engine) RoleFor(playerID string) (Role, bool) {
	role, assigned := g.roles[playerID]
	return role, assigned && (g.active || g.phase == GameComplete)
}

// KnownRolesFor returns only the role markers this player is allowed to see.
// Merlin sees every traitor by faction but not their special role.
func (g *Engine) KnownRolesFor(playerID string) map[string]Role {
	known := make(map[string]Role)
	role, assigned := g.RoleFor(playerID)
	if !assigned {
		return known
	}
	if role == Merlin {
		known[playerID] = Merlin
		for id, candidateRole := range g.roles {
			if isTraitor(candidateRole) {
				known[id] = Traitor
			}
		}
	} else if role == Assassin {
		known[playerID] = Assassin
	}
	return known
}

func (g *Engine) HasVoted(playerID string) bool {
	_, voted := g.proposalVotes[playerID]
	return voted
}

func (g *Engine) HasPlayedQuestCard(playerID string) bool {
	_, played := g.questCards[playerID]
	return played
}

func (g *Engine) Export() PersistedState {
	return PersistedState{
		Active: g.active, Generation: g.generation,
		Players: append([]Player(nil), g.players...), Roles: copyRoles(g.roles), CaptainIndex: g.captainIndex,
		Phase: g.phase, Round: g.round, Quest: append([]Player(nil), g.quest...),
		ProposalVotes: copyChoices(g.proposalVotes), RejectedTeams: g.rejectedTeams,
		QuestCards: copyChoices(g.questCards), Successful: g.successful, Failed: g.failed,
		LastProposal: copyProposal(g.lastProposal), LastQuest: copyQuest(g.lastQuest),
		QuestResults: append([]QuestResult(nil), g.questResults...), Winner: g.winner,
		Traitors: append([]Player(nil), g.traitors...), Assassination: copyAssassination(g.assassination), Settings: g.settings,
	}
}

// Restore replaces the engine with a validated durable snapshot while
// retaining its cryptographically secure random chooser for future games.
func (g *Engine) Restore(state PersistedState) error {
	state = normalizeLegacyRoles(state)
	playerByID, err := validatePersistedState(state)
	if err != nil {
		return err
	}
	g.active = state.Active
	g.generation = state.Generation
	g.players = append([]Player(nil), state.Players...)
	g.playerByID = playerByID
	g.roles = copyRoles(state.Roles)
	g.captainIndex = state.CaptainIndex
	g.phase = state.Phase
	g.round = state.Round
	g.quest = append([]Player(nil), state.Quest...)
	g.proposalVotes = copyChoices(state.ProposalVotes)
	g.rejectedTeams = state.RejectedTeams
	g.questCards = copyChoices(state.QuestCards)
	g.successful = state.Successful
	g.failed = state.Failed
	g.lastProposal = copyProposal(state.LastProposal)
	g.lastQuest = copyQuest(state.LastQuest)
	g.questResults = append([]QuestResult(nil), state.QuestResults...)
	g.winner = state.Winner
	g.traitors = append([]Player(nil), state.Traitors...)
	g.assassination = copyAssassination(state.Assassination)
	g.settings = state.Settings
	return nil
}

// normalizeLegacyRoles upgrades rooms saved before special roles or dead-player
// state were introduced so an in-progress room can still be restored safely.
func normalizeLegacyRoles(state PersistedState) PersistedState {
	legacySettings := len(state.Players) > 0 && state.Settings == (Settings{})
	if legacySettings {
		state.Settings = DefaultSettings(len(state.Players))
	}
	hasMerlin, hasAssassin := false, false
	for _, role := range state.Roles {
		hasMerlin = hasMerlin || role == Merlin
		hasAssassin = hasAssassin || role == Assassin
	}
	if len(state.Players) > 0 && !hasMerlin && !hasAssassin && legacySettings {
		state.Roles = copyRoles(state.Roles)
		for id, role := range state.Roles {
			if role == Traitor {
				state.Roles[id] = Assassin
			}
		}
		for _, player := range state.Players {
			if state.Roles[player.ID] == Innocent {
				state.Roles[player.ID] = Merlin
				break
			}
		}
	}

	if state.Assassination == nil {
		return state
	}
	targetID := state.Assassination.Target.ID
	state.Players = append([]Player(nil), state.Players...)
	for index := range state.Players {
		if state.Players[index].ID == targetID {
			state.Players[index].Dead = true
			break
		}
	}
	state.Assassination = copyAssassination(state.Assassination)
	state.Assassination.Target.Dead = true
	state.Traitors = append([]Player(nil), state.Traitors...)
	for index := range state.Traitors {
		if state.Traitors[index].ID == targetID {
			state.Traitors[index].Dead = true
		}
	}
	if !state.Assassination.Correct && state.Active {
		state.Phase = ChoosingTeam
		state.Quest = nil
		state.ProposalVotes = nil
		state.QuestCards = nil
		state.LastProposal = nil
		if state.Players[state.CaptainIndex].Dead {
			for range len(state.Players) {
				state.CaptainIndex = (state.CaptainIndex + 1) % len(state.Players)
				if !state.Players[state.CaptainIndex].Dead {
					break
				}
			}
		}
	}
	return state
}

func (g *Engine) Snapshot() Snapshot {
	questSizes := make([]int, TotalRounds)
	for round := 1; round <= TotalRounds; round++ {
		questSizes[round-1], _ = QuestSizeFor(g.livingPlayerCount(), round)
	}
	state := Snapshot{
		Active: g.active, Phase: g.phase, Round: g.round, TotalRounds: TotalRounds,
		Players:           append([]Player(nil), g.players...),
		Quest:             append([]Player(nil), g.quest...),
		QuestSize:         g.currentQuestSize(),
		QuestSizes:        questSizes,
		ProposalVotesCast: len(g.proposalVotes), ProposalVotesNeeded: g.livingPlayerCount(),
		RejectedProposals: g.rejectedTeams, ProposalRejectLimit: ProposalRejectionLimit,
		QuestCardsPlayed: len(g.questCards), QuestCardsNeeded: len(g.quest),
		SuccessfulQuests: g.successful, FailedQuests: g.failed,
		QuestResults: append([]QuestResult(nil), g.questResults...),
		LastProposal: copyProposal(g.lastProposal), LastQuest: copyQuest(g.lastQuest), Winner: g.winner,
		Assassination: copyAssassination(g.assassination),
	}
	if len(g.players) > 0 {
		state.Captain = g.players[g.captainIndex]
	}
	for _, player := range g.quest {
		if _, submitted := g.questCards[player.ID]; submitted {
			state.SubmittedQuestPlayers = append(state.SubmittedQuestPlayers, player.ID)
		}
	}
	if g.phase == GameComplete {
		state.Traitors = append([]Player(nil), g.traitors...)
	}
	return state
}

func (g *Engine) currentQuestSize() int {
	size, _ := QuestSizeFor(g.livingPlayerCount(), g.round)
	return size
}

// Cancel abandons an active game, for example when a player disconnects.
func (g *Engine) Cancel() bool {
	if !g.active {
		return false
	}
	g.active = false
	g.phase = ""
	return true
}

func (g *Engine) randomIndex(total int) (int, error) {
	index, err := g.choose(total)
	if err != nil {
		return 0, err
	}
	if index < 0 || index >= total {
		return 0, errors.New("random chooser returned an invalid player")
	}
	return index, nil
}

func (g *Engine) onQuest(playerID string) bool {
	for _, player := range g.quest {
		if player.ID == playerID {
			return true
		}
	}
	return false
}

func (g *Engine) isDead(playerID string) bool {
	player, exists := g.playerByID[playerID]
	return exists && player.Dead
}

func (g *Engine) livingPlayerCount() int {
	count := 0
	for _, player := range g.players {
		if !player.Dead {
			count++
		}
	}
	return count
}

func (g *Engine) markDead(playerID string) {
	player := g.playerByID[playerID]
	player.Dead = true
	g.playerByID[playerID] = player
	for index := range g.players {
		if g.players[index].ID == playerID {
			g.players[index] = player
			break
		}
	}
	for index := range g.traitors {
		if g.traitors[index].ID == playerID {
			g.traitors[index] = player
			break
		}
	}
}

func (g *Engine) advanceCaptain() {
	for range len(g.players) {
		g.captainIndex = (g.captainIndex + 1) % len(g.players)
		if !g.players[g.captainIndex].Dead {
			return
		}
	}
}

func (g *Engine) resolveQuest(result QuestResult) {
	g.lastQuest = &result
	g.questResults = append(g.questResults, result)
	if result.Succeeded {
		g.successful++
	} else {
		g.failed++
	}

	if g.successful == WinningQuests {
		g.finish(Innocent)
	} else if g.failed == WinningQuests {
		g.finish(Traitor)
	} else {
		g.round++
		g.advanceCaptain()
		g.phase = ChoosingTeam
		g.quest = nil
		g.proposalVotes = nil
		g.questCards = nil
	}
}

func (g *Engine) finish(winner Role) {
	g.active = false
	g.phase = GameComplete
	g.winner = winner
}

func copyRoles(source map[string]Role) map[string]Role {
	copy := make(map[string]Role, len(source))
	for id, role := range source {
		copy[id] = role
	}
	return copy
}

func copyChoices(source map[string]bool) map[string]bool {
	if source == nil {
		return nil
	}
	copy := make(map[string]bool, len(source))
	for id, choice := range source {
		copy[id] = choice
	}
	return copy
}

func validatePersistedState(state PersistedState) (map[string]Player, error) {
	players := make(map[string]Player, len(state.Players))
	deadPlayers := make([]Player, 0, 1)
	for _, player := range state.Players {
		if player.ID == "" || player.Name == "" {
			return nil, errors.New("persisted game contains an incomplete player")
		}
		if _, duplicate := players[player.ID]; duplicate {
			return nil, fmt.Errorf("persisted game contains duplicate player %q", player.ID)
		}
		players[player.ID] = player
		if player.Dead {
			deadPlayers = append(deadPlayers, player)
		}
	}
	if len(players) == 0 {
		if state.Active || state.Phase != "" || len(state.Roles) != 0 {
			return nil, errors.New("empty persisted game contains active state")
		}
		return players, nil
	}
	if state.CaptainIndex < 0 || state.CaptainIndex >= len(state.Players) {
		return nil, errors.New("persisted game has an invalid captain")
	}
	if state.Round < 1 || state.Round > TotalRounds {
		return nil, errors.New("persisted game has an invalid round")
	}
	if state.RejectedTeams < 0 || state.RejectedTeams >= ProposalRejectionLimit {
		return nil, errors.New("persisted game has an invalid rejection count")
	}
	if state.Successful < 0 || state.Successful > WinningQuests || state.Failed < 0 || state.Failed > WinningQuests {
		return nil, errors.New("persisted game has an invalid score")
	}
	traitorCount := 0
	minionCount := 0
	innocentCount := 0
	merlinCount := 0
	assassinCount := 0
	for id, role := range state.Roles {
		if _, exists := players[id]; !exists || !validRole(role) {
			return nil, errors.New("persisted game has an invalid role assignment")
		}
		if isTraitor(role) {
			traitorCount++
		}
		if role == Traitor {
			minionCount++
		}
		if role == Innocent {
			innocentCount++
		}
		if role == Merlin {
			merlinCount++
		}
		if role == Assassin {
			assassinCount++
		}
	}
	if len(state.Roles) != len(players) {
		return nil, errors.New("persisted game is missing role assignments")
	}
	if err := state.Settings.Validate(len(players)); err != nil {
		return nil, errors.New("persisted game has invalid role settings")
	}
	if minionCount != state.Settings.Minions || innocentCount != state.Settings.Innocents ||
		merlinCount != state.Settings.Merlins || assassinCount != state.Settings.Assassins ||
		traitorCount != len(state.Traitors) {
		return nil, errors.New("persisted game role assignments do not match its settings")
	}
	if state.Active {
		if state.Phase != ChoosingTeam && state.Phase != VotingOnTeam && state.Phase != PlayingQuest {
			return nil, errors.New("active persisted game has an invalid phase")
		}
	} else if state.Phase != "" && state.Phase != GameComplete {
		return nil, errors.New("inactive persisted game has an invalid phase")
	}
	if state.Active && state.Players[state.CaptainIndex].Dead {
		return nil, errors.New("active persisted game has a dead captain")
	}
	quest := make(map[string]struct{}, len(state.Quest))
	for _, player := range state.Quest {
		canonical, exists := players[player.ID]
		if !exists || canonical.Name != player.Name {
			return nil, errors.New("persisted quest contains an unknown player")
		}
		if _, duplicate := quest[player.ID]; duplicate {
			return nil, errors.New("persisted quest contains a duplicate player")
		}
		quest[player.ID] = struct{}{}
	}
	for id := range state.ProposalVotes {
		if _, exists := players[id]; !exists {
			return nil, errors.New("persisted proposal contains an unknown voter")
		}
	}
	for id := range state.QuestCards {
		if _, exists := quest[id]; !exists {
			return nil, errors.New("persisted quest contains an unknown card")
		}
	}
	for _, traitor := range state.Traitors {
		if !isTraitor(state.Roles[traitor.ID]) || players[traitor.ID].Name != traitor.Name {
			return nil, errors.New("persisted game contains an invalid traitor")
		}
	}
	if state.Assassination != nil {
		attempt := state.Assassination
		if state.Roles[attempt.Assassin.ID] != Assassin || players[attempt.Assassin.ID].Name != attempt.Assassin.Name ||
			attempt.Target.ID == attempt.Assassin.ID || players[attempt.Target.ID].Name != attempt.Target.Name ||
			attempt.Correct != (state.Roles[attempt.Target.ID] == Merlin) || !players[attempt.Target.ID].Dead ||
			!attempt.Target.Dead {
			return nil, errors.New("persisted game contains an invalid assassination")
		}
		if len(deadPlayers) != 1 || deadPlayers[0].ID != attempt.Target.ID {
			return nil, errors.New("persisted game contains an invalid dead player")
		}
		if attempt.Correct && (state.Active || state.Phase != GameComplete || state.Winner != Traitor) {
			return nil, errors.New("persisted game did not finish after Merlin was assassinated")
		}
		if !attempt.Correct && state.Active && state.Phase != ChoosingTeam {
			return nil, errors.New("persisted game did not return to team selection after a missed assassination")
		}
	} else if len(deadPlayers) != 0 {
		return nil, errors.New("persisted game contains a dead player without an assassination")
	}
	if state.Phase == GameComplete && state.Winner != Innocent && state.Winner != Traitor {
		return nil, errors.New("completed persisted game has no winner")
	}
	return players, nil
}

func copyProposal(result *ProposalResult) *ProposalResult {
	if result == nil {
		return nil
	}
	copy := *result
	return &copy
}

func copyQuest(result *QuestResult) *QuestResult {
	if result == nil {
		return nil
	}
	copy := *result
	return &copy
}

func copyAssassination(result *AssassinationResult) *AssassinationResult {
	if result == nil {
		return nil
	}
	copy := *result
	return &copy
}

func validRole(role Role) bool {
	return role == Innocent || role == Traitor || role == Merlin || role == Assassin
}

func isTraitor(role Role) bool {
	return role == Traitor || role == Assassin
}
