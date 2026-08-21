package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Client is the small common boundary shared by the project's DeepSeek clients.
type Client interface{ ChatCompletion(string) (string, error) }

// CompileRule asks the model to summarize one already-bounded evidence packet.
// It returns a proposal only; templatecontract.ApplyCandidateRule remains the
// required Go-side gate before a rule can affect formatting.
func CompileRule(ctx context.Context, client Client, request Request) (CandidateRule, error) {
	if ctx == nil || client == nil {
		return CandidateRule{}, fmt.Errorf("rule compiler context or client is nil")
	}
	if request.Task != "compile_format_rule" || request.Subject.CandidateRole == "" || len(request.Evidence) == 0 {
		return CandidateRule{}, fmt.Errorf("invalid structured rule request")
	}
	if err := ctx.Err(); err != nil {
		return CandidateRule{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return CandidateRule{}, err
	}
	prompt := `Compile exactly one thesis formatting rule from the structured evidence below. Return JSON only. Do not add a constraint unless cited evidence directly supports it. source_evidence_ids must contain only IDs present in evidence. Allowed constraints: font (east_asia, ascii), font.east_asia, font.ascii, size_pt, alignment (left/right/center/both/justify/distribute), spacing_before_pt, spacing_after_pt, line_spacing_pt, line_rule (auto/exact/atLeast), page_break_before. Format: {"rule_id":"...","selector":{"role":"...","variant":"..."},"constraints":{},"source_evidence_ids":["ev:..."],"confidence":0.0,"uncertainties":[]}
Request:
` + string(payload)
	response, err := client.ChatCompletion(prompt)
	if err != nil {
		return CandidateRule{}, err
	}
	var candidate CandidateRule
	response = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(response), "```json"), "```"), "```"))
	if err := json.Unmarshal([]byte(response), &candidate); err != nil {
		return CandidateRule{}, fmt.Errorf("structured rule compiler invalid JSON: %w", err)
	}
	candidate, err = NormalizeRuleCandidate(candidate)
	if err != nil {
		return CandidateRule{}, err
	}
	if err := ValidateRuleCandidate(request, candidate); err != nil {
		return CandidateRule{}, err
	}
	return candidate, nil
}
