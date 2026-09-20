package main

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
	"gophermind/internal/config"
	"gophermind/internal/core/service"
	postgresrepo "gophermind/internal/repo/postgres"
)

type teamRouteRuleConfig struct {
	ID              string   `json:"id"`
	Priority        int      `json:"priority"`
	Phrases         []string `json:"phrases"`
	Risk            string   `json:"risk"`
	Task            string   `json:"task"`
	WorkflowVersion string   `json:"workflow_version"`
}

func buildTeamQueryApplication(cfg config.TeamQueryConfig, db *gorm.DB, models service.ModelRouter) (*service.TeamQueryApplication, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if db == nil || cfg.TenantID == "" || cfg.FastModelType == "" || cfg.AdvancedModelType == "" || cfg.RulesJSON == "" {
		return nil, fmt.Errorf("TEAM_QUERY requires tenant, fast model, advanced model, and rules")
	}
	var raw []teamRouteRuleConfig
	if err := json.Unmarshal([]byte(cfg.RulesJSON), &raw); err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("invalid TEAM_QUERY_RULES_JSON")
	}
	rules := make([]service.TrustedQueryRouteRule, 0, len(raw))
	for _, rule := range raw {
		rules = append(rules, service.TrustedQueryRouteRule{ID: rule.ID, Priority: rule.Priority, Phrases: rule.Phrases, Route: gateway.Request{Risk: gateway.RiskLevel(rule.Risk), Task: gateway.TaskKind(rule.Task), WorkflowVersion: rule.WorkflowVersion}})
	}
	policy, err := service.NewTrustedQueryPolicy(rules)
	if err != nil {
		return nil, err
	}
	router, err := gateway.NewRouter(gateway.ModelRoute{ModelType: cfg.FastModelType, Thinking: gateway.ThinkingLow}, gateway.ModelRoute{ModelType: cfg.AdvancedModelType, Thinking: gateway.ThinkingHigh})
	if err != nil {
		return nil, err
	}
	capability, err := gateway.NewCapabilityPolicy([]gateway.Grant{
		{AgentID: runtime.TeamAgentEvidence, Capability: "model.generate"}, {AgentID: runtime.TeamAgentSafety, Capability: "model.generate"}, {AgentID: runtime.TeamAgentResponse, Capability: "model.generate"}, {AgentID: runtime.TeamAgentResponse, Capability: "response.commit"},
	})
	if err != nil {
		return nil, err
	}
	executor, err := gateway.NewExecutor(capability)
	if err != nil {
		return nil, err
	}
	fast, err := registerModelTeam(executor, models, cfg.FastModelType, "fast")
	if err != nil {
		return nil, err
	}
	advanced, err := registerModelTeam(executor, models, cfg.AdvancedModelType, "advanced")
	if err != nil {
		return nil, err
	}
	human := runtime.FixedTeam{Intake: pureTeamAgent, Triage: pureTriageAgent, Evidence: fast.Evidence, Safety: fast.Safety, Response: fast.Response}
	starter := service.PathFixedTeamStarter{Simple: postgresrepo.NewFixedTeamRuntime(db, fast, 0), Standard: postgresrepo.NewFixedTeamRuntime(db, advanced, 0), Human: postgresrepo.NewFixedTeamRuntime(db, human, 0)}
	barrier := &service.ResponseCommitBarrier{Verifier: postgresrepo.PostgresSafetyReviewVerifier{Tasks: postgresrepo.NewTaskDAGStore(db)}, Authorizer: capability, Committer: postgresrepo.NewCommittedResponseOutboxCommitter(db), AgentID: runtime.TeamAgentResponse, Capability: "response.commit", Purpose: "commit reviewed team response"}
	return &service.TeamQueryApplication{Policy: policy, Team: service.NewRoutedTeamQueryService(router, starter), Barrier: barrier, Reader: postgresrepo.NewCommittedResponseStore(db)}, nil
}

func registerModelTeam(executor *gateway.Executor, models service.ModelRouter, modelType, prefix string) (runtime.FixedTeam, error) {
	ids := map[string]string{"evidence": "team-" + prefix + "-evidence", "safety": "team-" + prefix + "-safety", "response": "team-" + prefix + "-response"}
	for role, id := range ids {
		if err := executor.Register(gateway.Manifest{ID: id, Kind: gateway.ExecutableSkill, Capability: "model.generate", MaxOutputBytes: 1 << 20}, service.ModelTeamExecutable(models, modelType, role)); err != nil {
			return runtime.FixedTeam{}, err
		}
	}
	return runtime.FixedTeam{Intake: pureTeamAgent, Triage: pureTriageAgent, Evidence: gateway.AuthorizedTeamAgent(executor, runtime.TeamAgentEvidence, ids["evidence"], "retrieve reviewed evidence"), Safety: gateway.AuthorizedTeamAgent(executor, runtime.TeamAgentSafety, ids["safety"], "review response safety"), Response: gateway.AuthorizedTeamAgent(executor, runtime.TeamAgentResponse, ids["response"], "produce reviewed response")}, nil
}

func pureTeamAgent(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	return append(json.RawMessage(nil), input...), nil
}
func pureTriageAgent(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"triage":"trusted_policy_routed"}`), nil
}
