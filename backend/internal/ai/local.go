package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/mafafd/backend/internal/models"
)

// LocalProvider is a deterministic heuristic fallback so the pipeline runs
// without external AI keys (demo mode). It produces a narrative, analyst-style
// explanation grounded in the transaction's real details (merchant, payment
// channel, location, amount) and the deterministic rule findings.
type LocalProvider struct{}

func NewLocalProvider() *LocalProvider { return &LocalProvider{} }

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) Analyze(ctx context.Context, in Input) (*Result, error) {
	score := in.DeterministicScore
	verdict := classifyVerdict(score)
	explanation := LocalExplain(in.Transaction, in.RuleBreakdown, in.VelocityCounts, score)
	return &Result{Score: score, Verdict: verdict, Explanation: explanation}, nil
}

// LocalExplain builds a narrative assessment from the transaction's own data
// and the deterministic findings. Shared by the worker and the seed so the demo
// incidents read like a real analyst wrote them.
func LocalExplain(tx *models.Transaction, breakdown []models.RuleScore, velocity map[string]int64, deterministic float64) string {
	if tx == nil {
		return fmt.Sprintf("No transaction context was available for review. Deterministic risk score %.0f/100.", deterministic)
	}
	verdict := classifyVerdict(deterministic)

	var b strings.Builder

	// 1) Context — what the purchase actually was.
	merchant := tx.MerchantName
	if merchant == "" {
		merchant = tx.MerchantCategory
	}
	if merchant == "" {
		merchant = "an online merchant"
	}
	loc := locationPhrase(tx.City, tx.CountryCode)
	fmt.Fprintf(&b, "A payment of %s was made at %s%s in %s, settled via %s.",
		formatAmount(tx.Amount, tx.Currency), merchant, categoryPhrase(tx.MerchantCategory), loc, paymentDescription(tx.PaymentMethod))

	// 2) Evidence — every rule that fired, cited precisely.
	var findings []string
	for _, r := range breakdown {
		if r.Score <= 0 {
			continue
		}
		findings = append(findings, findingSentence(r))
	}
	if len(findings) > 0 {
		b.WriteString(" Risk indicators: ")
		b.WriteString(strings.Join(findings, "; "))
		b.WriteString(".")
	} else {
		b.WriteString(" No risk thresholds were exceeded — the merchant, amount, location and payment channel are consistent with this customer's usual behavior.")
		if n := velocitySummary(velocity); n != "" {
			b.WriteString(" Recent velocity: " + n + ".")
		}
	}

	// 3) Synthesis — why the score maps to the verdict.
	fmt.Fprintf(&b, " Aggregating the weighted signals yields a deterministic score of %.0f/100, which maps to a %s verdict.",
		deterministic, verdict)
	return b.String()
}

func classifyVerdict(score float64) string {
	switch {
	case score >= 70:
		return "HIGH"
	case score >= 30:
		return "SUSPICIOUS"
	default:
		return "LOW"
	}
}

func locationPhrase(city, country string) string {
	switch {
	case city != "" && country != "":
		return city + ", " + strings.ToUpper(country)
	case country != "":
		return strings.ToUpper(country)
	case city != "":
		return city
	default:
		return "an unspecified location"
	}
}

func categoryPhrase(cat string) string {
	if cat == "" {
		return ""
	}
	labels := map[string]string{
		"online_retail":   "online retail",
		"fashion":         "apparel",
		"crypto_exchange": "crypto exchange",
		"ride_sharing":    "ride-hailing",
		"food_delivery":   "food delivery",
		"coffee_shop":     "coffee shop",
		"grocery":         "grocery",
		"digital_service": "digital subscription",
		"digital_goods":   "digital goods",
		"travel":          "travel",
		"electronics":     "electronics",
		"telecom":         "telecom",
		"bill_payment":    "utility bill",
		"freelance":       "freelance platform",
	}
	if l, ok := labels[cat]; ok {
		return " (" + l + ")"
	}
	return " (" + cat + ")"
}

func paymentDescription(code string) string {
	desc := map[string]string{
		"e_wallet_gopay":     "a GoPay e-wallet",
		"e_wallet_ovo":       "an OVO e-wallet",
		"e_wallet_dana":      "a DANA e-wallet",
		"e_wallet_shopeepay": "a ShopeePay e-wallet",
		"e_wallet_linkaja":   "a LinkAja e-wallet",
		"card_visa":          "a Visa card",
		"card_mastercard":    "a Mastercard",
		"card_jcb":           "a JCB card",
		"bank_transfer":      "a bank transfer",
		"paypal":             "PayPal",
		"apple_pay":          "Apple Pay",
		"google_pay":         "Google Pay",
	}
	if d, ok := desc[code]; ok {
		return d
	}
	if code == "" {
		return "an unknown payment channel"
	}
	return code
}

// findingSentence turns one fired rule into a natural analyst observation,
// keeping the exact numbers from the engine's detail string.
func findingSentence(r models.RuleScore) string {
	d := strings.ReplaceAll(r.Detail, "txns", "transactions")
	switch r.Rule {
	case "velocity_customer":
		return fmt.Sprintf("this customer is transacting at an unusually high rate (%s)", d)
	case "velocity_ip":
		return fmt.Sprintf("many transactions share the same source IP (%s)", d)
	case "velocity_card":
		return fmt.Sprintf("the payment instrument shows a burst of recent use (%s)", d)
	case "velocity_device":
		return fmt.Sprintf("the account is being used from several devices at once (%s)", d)
	case "velocity_global":
		return fmt.Sprintf("overall platform volume is elevated at this moment (%s)", d)
	case "geo_blocklist":
		return fmt.Sprintf("the purchase originates from a blocklisted country (%s)", d)
	case "geo_unknown":
		return fmt.Sprintf("the purchase originates from a country outside the account's usual regions (%s)", d)
	case "ip_blacklist":
		return fmt.Sprintf("the source IP is listed on the known-abuse blacklist (%s)", d)
	case "amount_max":
		return fmt.Sprintf("the amount is far larger than a typical single payment (%s)", d)
	case "amount_suspicious":
		return fmt.Sprintf("the spend sits above the suspicious-payment threshold (%s)", d)
	case "currency_mismatch":
		return fmt.Sprintf("the settlement currency differs from the account's base currency (%s)", d)
	default:
		return fmt.Sprintf("%s (%s)", r.Rule, d)
	}
}

// velocitySummary gives the recent activity context when no rule actually fired.
func velocitySummary(v map[string]int64) string {
	if len(v) == 0 {
		return ""
	}
	var parts []string
	phrases := map[string]string{
		"customer":         "this customer recently",
		"ip":               "the same IP recently",
		"card":             "this card recently",
		"distinct_devices": "distinct devices recently",
		"global":           "tenant-wide traffic this minute",
	}
	for k, n := range v {
		if n <= 0 {
			continue
		}
		if p, ok := phrases[k]; ok {
			parts = append(parts, fmt.Sprintf("%d via %s", n, p))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func formatAmount(amount float64, currency string) string {
	return fmt.Sprintf("%s %s", currency, formatInt(int64(amount*100), 100))
}

// formatInt formats value/divisor with thousands grouping and 2 decimals.
func formatInt(value, divisor int64) string {
	whole := value / divisor
	frac := value % divisor
	s := fmt.Sprintf("%d.%02d", whole, frac)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	idx := strings.Index(s, ".")
	if idx <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var out []byte
	intPart := s[:idx]
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	res := string(out) + s[idx:]
	if neg {
		return "-" + res
	}
	return res
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
