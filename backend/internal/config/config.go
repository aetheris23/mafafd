package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for every service.
type Config struct {
	Env       string
	LogLevel  string
	Service   string
	Port      int
	Debug     bool

	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	DatabaseURL      string

	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDB       int
	RedisPoolSize int

	MQProvider string
	RabbitMQURL string

	JWTTTLHours   time.Duration
	JWTSecret     []byte
	WSTokenTTL    time.Duration
	TokenRatePerMin float64
	TokenBurst    float64

	SeedDemoData  bool
	AIProvider    string
	GeminiAPIKey  string
	GeminiModel   string
	OpenAIAPIKey  string
	OpenAIModel   string
	OpenAIBaseURL string
	AIFallbackLocal bool

	WorkerConsumers int
	MaxBodyBytes    int64

	FrontendOrigin string
}

// Load reads configuration from the environment (optionally loading a .env file).
func Load(service string) (*Config, error) {
	_ = godotenv.Load() // optional; missing file is fine
	c := &Config{
		Env:       envStr("ENV", "development"),
		LogLevel:  envStr("LOG_LEVEL", "info"),
		Service:   service,
		Port:      envInt("INGESTION_PORT", 8080),
		Debug:     envStr("ENV", "development") == "development",
	}

	if service == "gateway" {
		c.Port = envInt("GATEWAY_PORT", 8081)
	}

	c.PostgresHost = envStr("POSTGRES_HOST", "localhost")
	c.PostgresPort = envInt("POSTGRES_PORT", 5432)
	c.PostgresUser = envStr("POSTGRES_USER", "mafafd")
	c.PostgresPassword = envStr("POSTGRES_PASSWORD", "mafafd")
	c.PostgresDB = envStr("POSTGRES_DB", "mafafd")
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	if c.DatabaseURL == "" {
		c.DatabaseURL = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort, c.PostgresDB,
		)
	}

	c.RedisHost = envStr("REDIS_HOST", "localhost")
	c.RedisPort = envInt("REDIS_PORT", 6379)
	c.RedisPassword = envStr("REDIS_PASSWORD", "")
	c.RedisDB = envInt("REDIS_DB", 0)
	c.RedisPoolSize = envInt("REDIS_POOL_SIZE", 512)

	c.MQProvider = envStr("MQ_PROVIDER", "redis")
	c.RabbitMQURL = envStr("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	c.JWTTTLHours = time.Duration(envInt("JWT_TTL_HOURS", 12)) * time.Hour
	c.WSTokenTTL = time.Duration(envInt("WS_TOKEN_TTL_SECONDS", 600)) * time.Second
	secret := envStr("JWT_SECRET", "")
	if len(secret) < 32 && c.Env == "production" {
		return nil, fmt.Errorf("JWT_SECRET must be >= 32 chars in production")
	}
	if secret == "" {
		secret = "dev-only-insecure-secret-change-me"
	}
	c.JWTSecret = []byte(secret)

	c.TokenRatePerMin = envFloat("TOKEN_BUCKET_RATE_PER_MIN", 600)
	c.TokenBurst = envFloat("TOKEN_BUCKET_BURST", 1200)

	c.SeedDemoData = envBool("SEED_DEMO_DATA", true)
	c.AIProvider = envStr("AI_PROVIDER", "local")
	c.GeminiAPIKey = envStr("GEMINI_API_KEY", "")
	c.GeminiModel = envStr("GEMINI_MODEL", "gemini-2.0-flash")
	c.OpenAIAPIKey = envStr("OPENAI_API_KEY", "")
	c.OpenAIModel = envStr("OPENAI_MODEL", "gpt-4o-mini")
	c.OpenAIBaseURL = envStr("OPENAI_BASE_URL", "https://api.openai.com/v1")
	c.AIFallbackLocal = envBool("AI_FALLBACK_LOCAL", true)

	c.WorkerConsumers = envInt("WORKER_CONSUMERS", 8)
	c.MaxBodyBytes = envInt64("MAX_BODY_BYTES", 32768)

	c.FrontendOrigin = envStr("FRONTEND_ORIGIN", "http://localhost:3000")

	return c, nil
}

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envInt64(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envFloat(k string, def float64) float64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}
