package ai

import (
	"context"
	"fmt"
	"strings"
)

// LocalProvider is a deterministic heuristic fallback so the pipeline runs
// without external AI keys (demo mode). It produces narrative explanations
// from the deterministic rule findings.
type LocalProvider struct{}

func NewLocalProvider() *LocalProvider { return &LocalProvider{} }

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) Analyze(ctx context.Context, in Input) (*Result, error) {
	score := in.DeterministicScore

	var signals []string
	for _, r := range in.RuleBreakdown {
		if r.Score > 0 {
			signals = append(signals, r.Detail)
		}
	}
	if len(in.VelocityCounts) > 0 {
		for k, v := range in.VelocityCounts {
			signals = append(signals, fmt.Sprintf("%s count=%d", k, v))
		}
	}

	verdict := "LOW"
	switch {
	case score >= 70:
		verdict = "HIGH"
	case score >= 30:
		verdict = "SUSPICIOUS"
	}

	explanation := "Heuristic assessment (no external LLM configured). "
	if len(signals) == 0 {
		explanation += "No risk signals exceeded configured thresholds."
	} else {
		explanation += "Signals observed: " + strings.Join(signals, "; ") + "."
	}
	explanation += fmt.Sprintf(" Aggregate risk score %0.1f maps to %s verdict.", score, verdict)

	return &Result{Score: score, Verdict: verdict, Explanation: explanation}, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
