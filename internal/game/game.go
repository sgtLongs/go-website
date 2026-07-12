// Package game contains the social-deduction rules and lifecycle. It knows
// nothing about WebSockets, rooms, or browser clients.
package game

import (
	"crypto/rand"
	"errors"
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
	ErrNotActive          = errors.New("no game is running")
	ErrWrongPhase         = errors.New("that action is not allowed right now")
	ErrNotCaptain         = errors.New("only the captain can choose the quest team")
	ErrInvalidQuest       = errors.New("choose the required number of different players")
	ErrMissingQuestRule   = errors.New("no quest rule is configured for these players")
	ErrNotProposalVoter   = errors.New("only players in this game may vote on the proposal")
	ErrAlreadyVoted       = errors.New("you have already voted")
	ErrNotOnQuest         = errors.New("you are not on this quest")
	ErrInnocentCannotFail = errors.New("innocent players cannot fail a quest")
)

type Role string

const (
	Innocent Role = "innocent"
	Traitor  Role = "traitor"
)

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

// Snapshot is public game information. It deliberately contains no roles or
// individual votes, so it is safe to broadcast to every player.
type Snapshot struct {
	Active                bool            `json:"active"`
	Phase                 Phase           `json:"phase"`
	Round                 int             `json:"round"`
	TotalRounds           int             `json:"totalRounds"`
	Players               []Player        `json:"players"`
	Captain               Player          `json:"captain"`
	Quest                 []Player        `json:"quest,omitempty"`
	QuestSize             int             `json:"questSize"`
	QuestSizes            []int           `json:"questSizes"`
	ProposalVotesCast     int             `json:"proposalVotesCast"`
	ProposalVotesNeeded   int             `json:"proposalVotesNeeded"`
	RejectedProposals     int             `json:"rejectedProposals"`
	ProposalRejectLimit   int             `json:"proposalRejectLimit"`
	QuestCardsPlayed      int             `json:"questCardsPlayed"`
	QuestCardsNeeded      int             `json:"questCardsNeeded"`
	SubmittedQuestPlayers []string        `json:"submittedQuestPlayers,omitempty"`
	SuccessfulQuests      int             `json:"successfulQuests"`
	FailedQuests          int             `json:"failedQuests"`
	QuestResults          []QuestResult   `json:"questResults"`
	LastProposal          *ProposalResult `json:"lastProposal,omitempty"`
	LastQuest             *QuestResult    `json:"lastQuest,omitempty"`
	Winner                Role            `json:"winner,omitempty"`
	Traitors              []Player        `json:"traitors,omitempty"`
}

type Started struct {
	Generation uint64
	Roles      map[string]Role
	State      Snapshot
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
	if g.active {
		return Started{}, ErrAlreadyActive
	}
	if len(players) < MinPlayers {
		return Started{}, ErrNotEnoughPlayers
	}
	for round := 1; round <= TotalRounds; round++ {
		if _, exists := QuestSizeFor(len(players), round); !exists {
			return Started{}, ErrMissingQuestRule
		}
	}

	traitorIndex, err := g.randomIndex(len(players))
	if err != nil {
		return Started{}, err
	}
	captainIndex, err := g.randomIndex(len(players))
	if err != nil {
		return Started{}, err
	}

	roles := make(map[string]Role, len(players))
	playerByID := make(map[string]Player, len(players))
	for _, player := range players {
		roles[player.ID] = Innocent
		playerByID[player.ID] = player
	}
	traitor := players[traitorIndex]
	roles[traitor.ID] = Traitor

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
	g.traitors = []Player{traitor}

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
	questSize := g.currentQuestSize()
	if len(playerIDs) != questSize {
		return ErrInvalidQuest
	}

	seen := make(map[string]struct{}, questSize)
	quest := make([]Player, 0, questSize)
	for _, id := range playerIDs {
		player, exists := g.playerByID[id]
		if _, duplicate := seen[id]; !exists || duplicate {
			return ErrInvalidQuest
		}
		seen[id] = struct{}{}
		quest = append(quest, player)
	}

	g.quest = quest
	g.proposalVotes = make(map[string]bool, len(g.players))
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
	if _, voted := g.proposalVotes[playerID]; voted {
		return false, ErrAlreadyVoted
	}

	g.proposalVotes[playerID] = approve
	if len(g.proposalVotes) < len(g.players) {
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
	if !g.onQuest(playerID) {
		return false, ErrNotOnQuest
	}
	if _, played := g.questCards[playerID]; played {
		return false, ErrAlreadyVoted
	}
	if !succeed && g.roles[playerID] != Traitor {
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

func (g *Engine) Active() bool { return g.active }

func (g *Engine) HasPlayer(playerID string) bool {
	_, exists := g.playerByID[playerID]
	return exists
}

func (g *Engine) RoleFor(playerID string) (Role, bool) {
	role, assigned := g.roles[playerID]
	return role, assigned && (g.active || g.phase == GameComplete)
}

func (g *Engine) Snapshot() Snapshot {
	questSizes := make([]int, TotalRounds)
	for round := 1; round <= TotalRounds; round++ {
		questSizes[round-1], _ = QuestSizeFor(len(g.players), round)
	}
	state := Snapshot{
		Active: g.active, Phase: g.phase, Round: g.round, TotalRounds: TotalRounds,
		Players:           append([]Player(nil), g.players...),
		Quest:             append([]Player(nil), g.quest...),
		QuestSize:         g.currentQuestSize(),
		QuestSizes:        questSizes,
		ProposalVotesCast: len(g.proposalVotes), ProposalVotesNeeded: len(g.players),
		RejectedProposals: g.rejectedTeams, ProposalRejectLimit: ProposalRejectionLimit,
		QuestCardsPlayed: len(g.questCards), QuestCardsNeeded: len(g.quest),
		SuccessfulQuests: g.successful, FailedQuests: g.failed,
		QuestResults: append([]QuestResult(nil), g.questResults...),
		LastProposal: copyProposal(g.lastProposal), LastQuest: copyQuest(g.lastQuest), Winner: g.winner,
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
	size, _ := QuestSizeFor(len(g.players), g.round)
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

func (g *Engine) advanceCaptain() {
	g.captainIndex = (g.captainIndex + 1) % len(g.players)
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
