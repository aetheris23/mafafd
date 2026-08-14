package telemetry

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitLogger configures a zerolog structured logger with service context.
func InitLogger(service, level string) zerolog.Logger {
	zl := zerolog.New(os.Stdout).With().Timestamp().Str("service", service).Logger()
	lv, err := zerolog.ParseLevel(level)
	if err != nil {
		lv = zerolog.InfoLevel
	}
	zl = zl.Level(lv)
	zerolog.TimeFieldFormat = time.RFC3339Nano
	if f := os.Getenv("LOG_FORMAT"); f == "console" || (f == "" && os.Getenv("ENV") == "development") {
		zl = zl.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})
	}
	log.Logger = zl
	return zl
}

// FatalIfErr exits with a clear message when a startup dependency fails.
func FatalIfErr(err error, msg string) {
	if err != nil {
		log.Fatal().Err(err).Msg(fmt.Sprintf("%s: %v", msg, err))
	}
}

// ServeMetrics runs a small HTTP server exposing Prometheus metrics on addr.
// Used by worker processes that do not host a public API.
func ServeMetrics(addr string, reg *prometheus.Registry, logger zerolog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	logger.Info().Str("addr", addr).Msg("metrics endpoint")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("metrics endpoint failed")
	}
}
