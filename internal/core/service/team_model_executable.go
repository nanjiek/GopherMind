package service

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/agent/gateway"
)

// ModelTeamExecutable creates a Gateway handler, not a direct worker. The
// handler is invoked only by gateway.Executor after its immediate Capability
// check. The model must return the required JSON object; prose is rejected.
func ModelTeamExecutable(router ModelRouter, modelType, role string) gateway.Handler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if router == nil || modelType == "" || role == "" {
			return nil, fmt.Errorf("model team executable is not configured")
		}
		prompt := "You are the fixed " + role + " worker. Return only one JSON object, with no markdown."
		if role == "safety" {
			prompt += " Approve only safe evidence. Your JSON must contain approved:true and review_context retaining the information the Response worker needs."
		}
		if role == "response" {
			prompt += " Your JSON must contain a non-empty answer derived only from the approved review context."
		}
		prompt += " Input:\n" + string(input)
		output, _, err := router.GenerateWithFallback(ctx, modelType, prompt)
		if err != nil {
			return nil, err
		}
		value := json.RawMessage(output)
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil || object == nil {
			return nil, fmt.Errorf("%s worker returned non-object JSON", role)
		}
		if role == "safety" {
			var approval struct {
				Approved bool `json:"approved"`
			}
			if json.Unmarshal(value, &approval) != nil || !approval.Approved {
				return nil, fmt.Errorf("safety worker did not explicitly approve")
			}
		}
		if role == "response" {
			var response struct {
				Answer string `json:"answer"`
			}
			if json.Unmarshal(value, &response) != nil || response.Answer == "" {
				return nil, fmt.Errorf("response worker returned no answer")
			}
		}
		return append(json.RawMessage(nil), value...), nil
	}
}
