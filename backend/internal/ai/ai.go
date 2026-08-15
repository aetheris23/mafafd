package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/security"
)

// Input is the fully-masked context handed to an LLM provider.
type Input struct {
	Transaction        *models.Transaction
	DeterministicScore float64
	RuleBreakdown      []models.RuleScore
	VelocityCounts     map[string]int64
	ProviderModel      string
}

// Result is the structured verdict returned by a provider.
type Result struct {
	Score       float64 `json:"ai_score"`
	Verdict     string  `json:"verdict"`
	Explanation string  `json:"explanation"`
	Raw         string  `json:"-"`
}

// Provider performs contextual fraud reasoning for suspicious transactions.
type Provider interface {
	Name() string
	Analyze(ctx context.Context, in Input) (*Result, error)
}

// MaskForExternal builds a JSON-safe, PII-free view of the input for the LLM.
// All identifiers are already hashed at the edge; IPs are further partially
// masked before anything leaves the cluster.
func MaskForExternal(in Input) map[string]any {
	t := in.Transaction
	out := map[string]any{
		"amount":               t.Amount,
		"currency":             t.Currency,
		"country":              t.CountryCode,
		"city":                 t.City,
		"merchant_category":    t.MerchantCategory,
		"merchant":             t.MerchantName,
		"payment_method":       t.PaymentMethod,
		"transaction_time_utc": t.TransactionTime.UTC().Format(time.RFC3339),
		"hour_of_day_utc":      t.TransactionTime.UTC().Hour(),
		"day_of_week_utc":      t.TransactionTime.UTC().Weekday().String(),
		"ip_masked":            security.MaskIP(t.IPAddress),
		"card_last4":           last4(t.CardToken),
		"deterministic_score":  in.DeterministicScore,
		"rule_breakdown":       in.RuleBreakdown,
		"velocity_counts":      in.VelocityCounts,
		"customer_id_hash":     truncate(t.CustomerIDHash),
		"device_hash":          truncate(t.DeviceFingerprintHash),
	}
	return out
}

func last4(card string) string {
	if card == "" {
		return ""
	}
	if i := strings.LastIndex(card, "****"); i >= 0 {
		return card[i+4:]
	}
	return strings.ToUpper(card[len(card)-4:])
}

func truncate(s string) string {
	if len(s) <= 16 {
		return s
	}
	return s[:16]
}

// Prompt builds the fraud-reasoning prompt in a deterministic template.
func Prompt(in Input) string {
	payload, _ := json.Marshal(MaskForExternal(in))
	var b strings.Builder
	b.WriteString("You are a senior financial fraud analyst. Assess this transaction for fraud risk.\n")
	b.WriteString("Return ONLY a JSON object with these exact keys: {\n")
	b.WriteString(`  "ai_score": <integer 0-100>,` + "\n")
	b.WriteString(`  "verdict": <"LOW" | "SUSPICIOUS" | "HIGH">,` + "\n")
	b.WriteString(`  "explanation": <2-4 sentence english reasoning citing the evidence>` + "\n}\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- ai_score must be at least the deterministic_score if evidence supports risk.\n")
	b.WriteString("- Escalate only on concrete evidence, not on a single data point.\n")
	b.WriteString("- Never include raw identifiers, hashes, or customer data in the explanation.\n\n")
	b.WriteString("Evidence (already anonymized):\n")
	b.WriteString(string(payload))
	b.WriteString("\n\nAnalyze now.")
	return b.String()
}

func (r *Result) validate() error {
	if r.Score < 0 || r.Score > 100 {
		return fmt.Errorf("ai_score out of range: %v", r.Score)
	}
	switch strings.ToUpper(r.Verdict) {
	case "LOW", "SUSPICIOUS", "HIGH":
		return nil
	default:
		return fmt.Errorf("invalid verdict: %q", r.Verdict)
	}
}
