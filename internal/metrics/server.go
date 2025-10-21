package metrics

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"net/http"
)

// StartServer starts an HTTP server to expose Prometheus metrics and a health check endpoint.
func StartServer(metricsPath string, port int, logger zerolog.Logger) error {
	http.Handle(metricsPath, promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	serverAddr := fmt.Sprintf(":%d", port)
	logger.Info().
		Str("addr", serverAddr).
		Str("metrics_path", metricsPath).
		Msg("starting HTTP server for Prometheus metrics")
	if err := http.ListenAndServe(serverAddr, nil); err != nil {
		logger.Error().Err(err).Msg("HTTP server failed")
		return err
	}
	return nil
}
