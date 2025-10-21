package drift

import (
	"context"
	"fmt"
	"github.com/01builders/ev-metrics/internal/metrics"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"time"
)

// Monitor continuously checks block heights of a reference node and multiple full nodes,
// recording the heights and their drift into the provided metrics instance at specified intervals.
func Monitor(ctx context.Context, m *metrics.Metrics, chainID string, referenceNode string, fullNodes []string, pollingInterval int, logger zerolog.Logger) error {
	ticker := time.NewTicker(time.Duration(pollingInterval) * time.Second)
	defer ticker.Stop()

	logger.Info().
		Str("reference_node", referenceNode).
		Strs("full_nodes", fullNodes).
		Int("polling_interval_sec", pollingInterval).
		Msg("starting node drift monitoring")

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("stopping node drift monitoring")
			return ctx.Err()
		case <-ticker.C:
			// get reference node height
			refHeight, err := getBlockHeight(ctx, referenceNode)
			if err != nil {
				logger.Error().Err(err).Str("endpoint", referenceNode).Msg("failed to get reference node block height")
				continue
			}

			m.RecordReferenceBlockHeight(chainID, referenceNode, refHeight)
			logger.Info().Uint64("height", refHeight).Str("endpoint", referenceNode).Msg("recorded reference node height")

			// get each full node height and calculate drift
			for _, fullNode := range fullNodes {
				currentHeight, err := getBlockHeight(ctx, fullNode)
				if err != nil {
					logger.Error().Err(err).Str("endpoint", fullNode).Msg("failed to get full node block height")
					continue
				}

				m.RecordCurrentBlockHeight(chainID, fullNode, currentHeight)
				m.RecordBlockHeightDrift(chainID, fullNode, refHeight, currentHeight)

				drift := int64(refHeight) - int64(currentHeight)
				logger.Info().
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
