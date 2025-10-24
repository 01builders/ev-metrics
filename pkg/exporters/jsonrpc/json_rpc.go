package jsonrpc

import (
	"context"
	"github.com/01builders/ev-metrics/internal/clients/evm"
	"github.com/01builders/ev-metrics/pkg/metrics"
	"time"

	"github.com/rs/zerolog"
)

var _ metrics.Exporter = &exporter{}

func NewMetricsExporter(chainID string, evmClient *evm.Client, scrapeInterval int, logger zerolog.Logger) metrics.Exporter {
	return &exporter{
		chainID:        chainID,
		evmClient:      evmClient,
		scrapeInterval: scrapeInterval,
		logger:         logger,
	}
}

type exporter struct {
	chainID        string
	evmClient      *evm.Client
	scrapeInterval int
	logger         zerolog.Logger
}

func (g *exporter) ExportMetrics(ctx context.Context, m *metrics.Metrics) error {
	logger := g.logger.With().Str("component", "jsonrpc_monitor").Logger()
	logger.Info().
		Str("chain_id", g.chainID).
		Int("scrape_interval_seconds", g.scrapeInterval).
		Msg("starting JSON-RPC health monitoring")

	// Initialize SLO threshold gauges once at startup
	m.InitializeJsonRpcSloThresholds(g.chainID)

	ticker := time.NewTicker(time.Duration(g.scrapeInterval) * time.Second)
	defer ticker.Stop()

	// Perform initial health check immediately
	if err := performHealthCheck(ctx, m, g.chainID, g.evmClient, logger); err != nil {
		logger.Warn().Err(err).Msg("initial health check failed")
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("stopping JSON-RPC health monitoring")
			return ctx.Err()
		case <-ticker.C:
			if err := performHealthCheck(ctx, m, g.chainID, g.evmClient, logger); err != nil {
				logger.Warn().Err(err).Msg("health check failed")
			}
		}
	}
}

func performHealthCheck(
	ctx context.Context,
	m *metrics.Metrics,
	chainID string,
	evmClient *evm.Client,
	logger zerolog.Logger,
) error {
	duration, err := evmClient.HealthCheckRequest(ctx)
	if err != nil {
		return err
	}

	m.RecordJsonRpcRequestDuration(chainID, duration)

	logger.Info().
		Dur("duration", duration).
		Float64("duration_seconds", duration.Seconds()).
		Msg("JSON-RPC health check completed")

	return nil
}
