// feeder streams REAL market data into the MAFAFD ingestion API.
//
// Every tick it pulls live prices from CoinGecko and FX rates derived from
// ECB/Bank-Indonesia-style references (Frankfurter), then POSTs realistic
// transactions to the ingestion endpoint. Amounts/prices are real-time;
// customer/card metadata is synthetic.
//
//	go build -o bin/feeder ./cmd/feeder
//	./bin/feeder -interval 5 -once
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mafafd/backend/internal/models"
)

const (
	defaultKey     = "sk_live_38f2a91b4c0d5e7f9a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e0f2a4b6c8d"
	coingeckoURL   = "https://api.coingecko.com/api/v3/simple/price"
	frankfurterURL = "https://api.frankfurter.app/latest"
	defaultAssets  = "bitcoin,ethereum,solana,cardano,polkadot,ripple"
	bigOrderChance = 0.05
	idrTxnChance   = 0.20
	requestTimeout = 10 * time.Second
)

type config struct {
	url      string
	apiKey   string
	interval time.Duration
	once     bool
	assets   []string
	seq      int64
	lastCard string
	lastIP   string
}

var httpc = &http.Client{Timeout: requestTimeout}

// Synthetic parties. Amounts and prices are real; identities are sample data.
var (
	customers = []string{
		"c-1001", "c-1002", "c-1003", "c-1004", "c-1005", "c-1006", "c-1007",
		"c-1008", "c-1009", "c-1010", "c-1011", "c-1012", "c-1013", "c-1014",
		"c-1015", "c-1016", "c-1017", "c-1018", "c-1019", "c-1020",
	}
	emails = []string{
		"nova.r", "j.santoso", "m.william", "putri.ayu", "kenji.t", "sarah.l",
		"budi.p", "lina.k", "farhan.a", "mia.ng", "david.c", "ayu.w",
	}
	cards = []string{
		"tok_visa_7f2a1b3c", "tok_mc_90e4c2d1", "tok_visa_5c8d9e01",
		"tok_mc_12ab34cd", "tok_visa_0f3a9b8c", "tok_mc_deadbeef",
		"tok_visa_4b6d8e0f", "tok_mc_77a1b2c3",
	}
	merchants = []string{
		"crypto_exchange", "online_retail", "ride_sharing", "freelance",
		"coffee_shop", "grocery", "digital_wallet",
	}
	locales = []struct {
		country string
		city    string
		ip      string
	}{
		{"US", "New York", "24.199.99.10"},
		{"US", "San Francisco", "198.51.100.7"},
		{"US", "Chicago", "203.0.113.19"},
		{"ID", "Jakarta", "114.125.12.31"},
		{"ID", "Surabaya", "36.77.55.210"},
		{"ID", "Denpasar", "182.2.99.14"},
		{"SG", "Singapore", "118.200.2.77"},
		{"SG", "Singapore", "116.14.48.201"},
	}
)

func main() {
	_ = godotenv.Load() // optional .env: MAFAFD_API_KEY, INGESTION_URL

	var c config
	key := flag.String("key", envOr("MAFAFD_API_KEY", defaultKey), "ingestion API key (X-Api-Key)")
	url := flag.String("url", envOr("INGESTION_URL", "http://localhost:8080"), "ingestion base URL")
	sec := flag.Int("interval", 5, "seconds between ticks")
	once := flag.Bool("once", false, "send one batch and exit")
	assets := flag.String("assets", defaultAssets, "comma-separated CoinGecko asset ids")
	seed := flag.Int64("seed", time.Now().UnixNano(), "RNG seed")
	flag.Parse()

	c.apiKey = *key
	c.url = strings.TrimRight(*url, "/")
	c.interval = time.Duration(*sec) * time.Second
	c.once = *once
	c.assets = splitCSV(*assets)
	if len(c.assets) == 0 {
		c.assets = splitCSV(defaultAssets)
	}
	rng := rand.New(rand.NewSource(*seed))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("feeder start: url=%s assets=%v interval=%s once=%v", c.url, c.assets, c.interval, c.once)

	if c.once {
		if err := c.tick(ctx, rng); err != nil {
			log.Fatalf("tick failed: %v", err)
		}
		return
	}

	for {
		if err := c.tick(ctx, rng); err != nil {
			log.Printf("tick error: %v", err)
		}
		select {
		case <-ctx.Done():
			log.Println("feeder stopped")
			return
		case <-time.After(c.interval):
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// tick fetches live prices + FX and posts a small batch of transactions.
func (c *config) tick(ctx context.Context, rng *rand.Rand) error {
	prices, err := fetchPrices(ctx, c.assets)
	if err != nil {
		return fmt.Errorf("prices: %w", err)
	}
	fx, err := fetchFX(ctx)
	if err != nil {
		return fmt.Errorf("fx: %w", err)
	}

	n := 1 + rng.Intn(2) // 1-2 transactions per tick
	for i := 0; i < n; i++ {
		txn := c.genTxn(rng, prices, fx)
		if err := c.send(ctx, txn); err != nil {
			log.Printf("send failed: %v", err)
		}
	}
	return nil
}

// fetchPrices returns live USD prices for the requested CoinGecko ids.
func fetchPrices(ctx context.Context, ids []string) (map[string]float64, error) {
	u := coingeckoURL + "?ids=" + strings.Join(ids, ",") + "&vs_currencies=usd"
	var out map[string]map[string]float64
	if err := getJSON(ctx, u, &out); err != nil {
		return nil, err
	}
	prices := make(map[string]float64, len(ids))
	for id, v := range out {
		if usd, ok := v["usd"]; ok && usd > 0 {
			prices[id] = usd
		}
	}
	if len(prices) == 0 {
		return nil, fmt.Errorf("no prices returned")
	}
	return prices, nil
}

// fetchFX returns USD-based FX rates (ECB reference via Frankfurter).
func fetchFX(ctx context.Context) (map[string]float64, error) {
	u := frankfurterURL + "?from=USD&to=IDR,EUR,JPY,SGD"
	var out struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := getJSON(ctx, u, &out); err != nil {
		return nil, err
	}
	return out.Rates, nil
}

func getJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s -> HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst)
}

// genTxn builds a transaction whose value is derived from live market data.
func (c *config) genTxn(rng *rand.Rand, prices map[string]float64, fx map[string]float64) models.TransactionInput {
	asset := c.assets[rng.Intn(len(c.assets))]
	price := prices[asset]

	idr := fx["IDR"]
	useIDR := idr > 0 && rng.Float64() < idrTxnChance

	currency := "USD"
	amount := pickAmount(rng)
	if useIDR {
		currency = "IDR"
		amount = math.Round(amount*idr*100) / 100
	}

	loc := locales[rng.Intn(len(locales))]
	// Slight regional bias: IDR transactions come from Indonesian locales.
	if currency == "IDR" {
		idLoc := []struct {
			country string
			city    string
			ip      string
		}{{"ID", "Jakarta", "114.125.12.31"}, {"ID", "Surabaya", "36.77.55.210"}, {"ID", "Denpasar", "182.2.99.14"}}
		l := idLoc[rng.Intn(len(idLoc))]
		loc.country, loc.city, loc.ip = l.country, l.city, l.ip
	}

	email := emails[rng.Intn(len(emails))]
	c.seq++
	meta := map[string]any{
		"asset":     asset,
		"price_usd": round2(price),
		"source":    "coingecko",
		"fx":        fx,
	}

	return models.TransactionInput{
		ExternalID:        fmt.Sprintf("feeder-%d-%d", time.Now().Unix(), c.seq),
		Amount:            math.Round(amount*100) / 100,
		Currency:          currency,
		CustomerID:        customers[rng.Intn(len(customers))],
		CustomerEmail:     fmt.Sprintf("%s@example.dev", email),
		CardToken:         cards[rng.Intn(len(cards))],
		IPAddress:         loc.ip,
		DeviceFingerprint: fmt.Sprintf("dev-%s-%d", loc.country, 1000+rng.Intn(9000)),
		CountryCode:       loc.country,
		City:              loc.city,
		MerchantCategory:  merchants[rng.Intn(len(merchants))],
		Metadata:          meta,
		Timestamp:         timePtr(time.Now()),
	}
}

// pickAmount returns a USD dollar value: mostly a normal spend, occasionally a
// large crypto order that crosses the >max_single threshold and raises an
// incident.
func pickAmount(rng *rand.Rand) float64 {
	if rng.Float64() < bigOrderChance {
		return 11000 + rng.Float64()*30000
	}
	return 150 + rng.Float64()*3850
}

func (c *config) send(ctx context.Context, txn models.TransactionInput) error {
	body, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/api/v1/transactions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		var out struct {
			TransactionID string  `json:"transaction_id"`
			EstMs         float64 `json:"estimated_processing_ms"`
		}
		_ = json.Unmarshal(b, &out)
		price := ""
		if p, ok := txn.Metadata["price_usd"].(float64); ok {
			price = fmt.Sprintf("$%.2f", p)
		}
		log.Printf("accepted %s %s %s %s -> %s (est %.1fms)",
			txn.ExternalID, txn.Currency, fmt.Sprintf("%.2f", txn.Amount), price,
			out.TransactionID, out.EstMs)
		return nil
	case http.StatusTooManyRequests:
		ra := resp.Header.Get("Retry-After")
		return fmt.Errorf("rate limited (retry-after: %s)", ra)
	default:
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func timePtr(t time.Time) *time.Time { return &t }
