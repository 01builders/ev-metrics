package metrics

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"net/http"
)

// Exporter defines an interface for exporting metrics.
type Exporter interface {
	ExportMetrics(ctx context.Context, m *Metrics) error
}

// StartServer starts an HTTP server to expose Prometheus metrics and a health check endpoint.
func StartServer(ctx context.Context, metricsPath string, port int, logger zerolog.Logger, exporters ...Exporter) error {
	http.Handle(metricsPath, promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// initialize metrics
	m := New("ev_metrics")

	var g errgroup.Group
	for _, exporter := range exporters {
		// capture variable for closure
		exp := exporter
		g.Go(func() error {
			return exp.ExportMetrics(ctx, m)
		})
	}

	serverAddr := fmt.Sprintf(":%d", port)
	logger.Info().
		Str("addr", serverAddr).
		Str("metrics_path", metricsPath).
		Msg("starting HTTP server for Prometheus metrics")

	g.Go(func() error {
		return http.ListenAndServe(serverAddr, nil)
	})

	return g.Wait()
}
