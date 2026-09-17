package evaluation

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type goldCase struct {
	ID              string   `json:"id"`
	Query           string   `json:"query"`
	Intent          string   `json:"intent"`
	RiskLevel       string   `json:"risk_level"`
	RequiredAgents  []string `json:"required_agents"`
	ExpectedRoute   string   `json:"expected_route"`
	ExpectedTools   []string `json:"expected_tools"`
	KeyFacts        []string `json:"key_facts"`
	ForbiddenClaims []string `json:"forbidden_claims"`
	ShouldAbstain   bool     `json:"should_abstain"`
	HumanEscalation bool     `json:"human_escalation"`
	Tags            []string `json:"tags"`
}

func TestP0GoldSetSchema(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "evaluation", "p0_gold.jsonl")
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	seen := map[string]struct{}{}
	riskSeen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
		var item goldCase
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &item), "line %d", count)
		require.NotEmpty(t, item.ID)
		require.NotEmpty(t, item.Query)
		require.NotEmpty(t, item.Intent)
		require.Contains(t, []string{"L0", "L1", "L2", "L3"}, item.RiskLevel)
		require.NotEmpty(t, item.RequiredAgents)
		require.NotEmpty(t, item.ExpectedRoute)
		require.NotEmpty(t, item.KeyFacts)
		require.NotEmpty(t, item.ForbiddenClaims)
		_, duplicate := seen[item.ID]
		require.False(t, duplicate, "duplicate case id %s", item.ID)
		seen[item.ID] = struct{}{}
		riskSeen[item.RiskLevel] = true
	}
	require.NoError(t, scanner.Err())
	require.GreaterOrEqual(t, count, 12)
	for _, risk := range []string{"L0", "L1", "L2", "L3"} {
		require.True(t, riskSeen[risk], "missing risk level %s", risk)
	}
}
