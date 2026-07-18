package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed quest_rules.json
var questRulesJSON []byte

type questRule struct {
	min              int
	max              int
	recommendedRoles recommendedRoles
	rounds           map[string]roundRule
}

type recommendedRoles struct {
	Minions  int `json:"minions"`
	Servants int `json:"servants"`
	Assassin int `json:"assassin"`
	Merlin   int `json:"merlin"`
}

type roundRule struct {
	Players             int `json:"players"`
	PlayersNeededToFail int `json:"players_needed_to_fail"`
}

type questRuleSource struct {
	RecommendedRoles recommendedRoles     `json:"recommended_roles"`
	Rounds           map[string]roundRule `json:"rounds"`
}

var configuredQuestRules = mustLoadQuestRules(questRulesJSON)

// QuestSizeFor returns the configured quest-team size for a player count and
// round. The boolean is false when the rules file does not cover the request.
func QuestSizeFor(playerCount, round int) (int, bool) {
	if playerCount < 1 || round < 1 || round > TotalRounds {
		return 0, false
	}
	key := fmt.Sprintf("round_%d", round)
	for _, rule := range configuredQuestRules {
		if playerCount >= rule.min && playerCount <= rule.max {
			roundRule, exists := rule.rounds[key]
			return roundRule.Players, exists && roundRule.Players > 0 && roundRule.Players <= playerCount
		}
	}
	return 0, false
}

// QuestFailThresholdFor returns the configured number of failed cards needed
// to fail a quest for a player count and round.
func QuestFailThresholdFor(playerCount, round int) (int, bool) {
	if playerCount < 1 || round < 1 || round > TotalRounds {
		return 0, false
	}
	key := fmt.Sprintf("round_%d", round)
	for _, rule := range configuredQuestRules {
		if playerCount >= rule.min && playerCount <= rule.max {
			roundRule, exists := rule.rounds[key]
			return roundRule.PlayersNeededToFail, exists && roundRule.PlayersNeededToFail > 0 && roundRule.PlayersNeededToFail <= roundRule.Players
		}
	}
	return 0, false
}

func recommendedSettingsFor(playerCount int) (Settings, bool) {
	for _, rule := range configuredQuestRules {
		if playerCount < rule.min || playerCount > rule.max {
			continue
		}
		roles := rule.recommendedRoles
		settings := Settings{Minions: roles.Minions, Innocents: roles.Servants, Merlins: roles.Merlin, Assassins: roles.Assassin}
		// Open-ended ranges describe their minimum player count. Additional
		// players default to loyal servants so the recommendation stays valid.
		settings.Innocents += playerCount - settings.Total()
		return settings, settings.Minions >= 0 && settings.Innocents >= 0 && settings.Merlins >= 0 && settings.Assassins >= 0
	}
	return Settings{}, false
}

func mustLoadQuestRules(contents []byte) []questRule {
	var source map[string]questRuleSource
	if err := json.Unmarshal(contents, &source); err != nil {
		panic(fmt.Sprintf("game: invalid quest_rules.json: %v", err))
	}

	rules := make([]questRule, 0, len(source))
	for playerRange, sourceRule := range source {
		minPlayers, maxPlayers, err := parsePlayerRange(playerRange)
		if err != nil {
			panic(fmt.Sprintf("game: invalid player range %q: %v", playerRange, err))
		}
		for round := 1; round <= TotalRounds; round++ {
			key := fmt.Sprintf("round_%d", round)
			roundRule, exists := sourceRule.Rounds[key]
			if !exists || roundRule.Players < 1 || roundRule.Players > minPlayers {
				panic(fmt.Sprintf("game: %q must define %s between 1 and %d", playerRange, key, minPlayers))
			}
			if roundRule.PlayersNeededToFail < 1 || roundRule.PlayersNeededToFail > roundRule.Players {
				panic(fmt.Sprintf("game: %q %s players_needed_to_fail must be between 1 and %d", playerRange, key, roundRule.Players))
			}
		}
		roles := sourceRule.RecommendedRoles
		if roles.Minions < 0 || roles.Servants < 0 || roles.Assassin < 0 || roles.Merlin < 0 ||
			roles.Minions+roles.Servants+roles.Assassin+roles.Merlin != minPlayers {
			panic(fmt.Sprintf("game: %q recommended_roles must define exactly %d players", playerRange, minPlayers))
		}
		rules = append(rules, questRule{min: minPlayers, max: maxPlayers, recommendedRoles: roles, rounds: sourceRule.Rounds})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].min < rules[j].min })
	return rules
}

func parsePlayerRange(value string) (int, int, error) {
	if strings.HasSuffix(value, "+") {
		minPlayers, err := strconv.Atoi(strings.TrimSuffix(value, "+"))
		if err != nil || minPlayers < 1 {
			return 0, 0, fmt.Errorf("expected a positive range such as 11+")
		}
		return minPlayers, int(^uint(0) >> 1), nil
	}
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected a range such as 3-4")
	}
	minPlayers, minErr := strconv.Atoi(parts[0])
	maxPlayers, maxErr := strconv.Atoi(parts[1])
	if minErr != nil || maxErr != nil || minPlayers < 1 || maxPlayers < minPlayers {
		return 0, 0, fmt.Errorf("expected an ascending positive range")
	}
	return minPlayers, maxPlayers, nil
}
