package drift

import (
	"context"
	"fmt"
	"github.com/01builders/ev-metrics/pkg/metrics"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"time"
)

var _ metrics.Exporter = &exporter{}

// NewMetricsExporter creates and returns a metrics exporter for monitoring block height drift across multiple nodes.
func NewMetricsExporter(chainID, referenceNode string, fullNodes []string, pollingInterval int, logger zerolog.Logger) metrics.Exporter {
	return &exporter{
		chainID:         chainID,
		referenceNode:   referenceNode,
		fullNodes:       fullNodes,
		pollingInterval: pollingInterval,
		logger:          logger,
	}
}

// exporter represents an implementation of the metrics.Exporter interface for monitoring node block height data.
type exporter struct {
	chainID         string
	referenceNode   string
	fullNodes       []string
	pollingInterval int
	logger          zerolog.Logger
}

// GatherMetrics continuously checks block heights of a reference node and multiple full nodes,
// recording the heights and their drift into the provided metrics instance at specified intervals.
func (g exporter) ExportMetrics(ctx context.Context, m *metrics.Metrics) error {
	ticker := time.NewTicker(time.Duration(g.pollingInterval) * time.Second)
	defer ticker.Stop()

	g.logger.Info().
		Str("reference_node", g.referenceNode).
		Strs("full_nodes", g.fullNodes).
		Int("polling_interval_sec", g.pollingInterval).
		Msg("starting node drift monitoring")

	for {
		select {
		case <-ctx.Done():
			g.logger.Info().Msg("stopping node drift monitoring")
			return ctx.Err()
		case <-ticker.C:
			// get reference node height
			refHeight, err := getBlockHeight(ctx, g.referenceNode)
			if err != nil {
				g.logger.Error().Err(err).Str("endpoint", g.referenceNode).Msg("failed to get reference node block height")
				continue
			}

			m.RecordReferenceBlockHeight(g.chainID, g.referenceNode, refHeight)
			g.logger.Info().Uint64("height", refHeight).Str("endpoint", g.referenceNode).Msg("recorded reference node height")

			// get each full node height and calculate drift
			for _, fullNode := range g.fullNodes {
				currentHeight, err := getBlockHeight(ctx, fullNode)
				if err != nil {
					g.logger.Error().Err(err).Str("endpoint", fullNode).Msg("failed to get full node block height")
					continue
				}

				m.RecordCurrentBlockHeight(g.chainID, fullNode, currentHeight)
				m.RecordBlockHeightDrift(g.chainID, fullNode, refHeight, currentHeight)

				drift := int64(refHeight) - int64(currentHeight)
				g.logger.Info().
					Uint64("ref_height", refHeight).
					Uint64("target_height", currentHeight).
					Int64("drift", drift).
					Str("endpoint", fullNode).
					Msg("recorded full node height and drift")
			}
		}
	}
}

// getBlockHeight queries an EVM RPC endpoint for its current block height
func getBlockHeight(ctx context.Context, rpcURL string) (uint64, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to %s: %w", rpcURL, err)
	}
	defer client.Close()

	height, err := client.BlockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get block number from %s: %w", rpcURL, err)
	}

	return height, nil
}
