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
	min    int
	max    int
	rounds map[string]int
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
			size, exists := rule.rounds[key]
			return size, exists && size > 0 && size <= playerCount
		}
	}
	return 0, false
}

func mustLoadQuestRules(contents []byte) []questRule {
	var source map[string]map[string]int
	if err := json.Unmarshal(contents, &source); err != nil {
		panic(fmt.Sprintf("game: invalid quest_rules.json: %v", err))
	}

	rules := make([]questRule, 0, len(source))
	for playerRange, rounds := range source {
		minPlayers, maxPlayers, err := parsePlayerRange(playerRange)
		if err != nil {
			panic(fmt.Sprintf("game: invalid player range %q: %v", playerRange, err))
		}
		for round := 1; round <= TotalRounds; round++ {
			key := fmt.Sprintf("round_%d", round)
			size, exists := rounds[key]
			if !exists || size < 1 || size > minPlayers {
				panic(fmt.Sprintf("game: %q must define %s between 1 and %d", playerRange, key, minPlayers))
			}
		}
		rules = append(rules, questRule{min: minPlayers, max: maxPlayers, rounds: rounds})
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
