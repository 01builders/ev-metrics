package cmd

import (
	"context"
	"github.com/01builders/ev-metrics/internal/celestia"
	"github.com/01builders/ev-metrics/internal/evm"
	"github.com/01builders/ev-metrics/internal/evnode"
	"github.com/01builders/ev-metrics/internal/metrics"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"time"
)

// BlockVerifier handles verification of blocks against Celestia DA
type BlockVerifier struct {
	evnodeClient   *evnode.Client
	celestiaClient *celestia.Client
	evmClient      *evm.Client
	headerNS       []byte
	dataNS         []byte
	metrics        *metrics.Metrics
	chainID        string
	logger         zerolog.Logger
}

// NewBlockVerifier creates a new BlockVerifier
func NewBlockVerifier(
	evnodeClient *evnode.Client,
	celestiaClient *celestia.Client,
	evmClient *evm.Client,
	headerNS, dataNS []byte,
	metrics *metrics.Metrics,
	chainID string,
	logger zerolog.Logger,
) *BlockVerifier {
	return &BlockVerifier{
		evnodeClient:   evnodeClient,
		evmClient:      evmClient,
		celestiaClient: celestiaClient,
		headerNS:       headerNS,
		dataNS:         dataNS,
		metrics:        metrics,
		chainID:        chainID,
		logger:         logger,
	}
}

// VerifyHeadersAndData begins the background retry processor
func (v *BlockVerifier) VerifyHeadersAndData(ctx context.Context) error {
	headers := make(chan *types.Header, 10)
	sub, err := v.evmClient.SubscribeNewHead(ctx, headers)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
			// when a new unverified status is received, spawn a goroutine to handle retries.
		case header := <-headers:
			// spawn a goroutine to handle this block's retries
			go v.verifyBlock(ctx, header)
		}
	}
}

func (v *BlockVerifier) onVerified(namespace string, blockHeight, daHeight uint64, verified bool, submissionDuration time.Duration) {
	if verified {
		v.metrics.RecordSubmissionDaHeight(v.chainID, namespace, daHeight)
		v.metrics.RemoveVerifiedBlock(v.chainID, namespace, blockHeight)
		v.metrics.RecordSubmissionDuration(v.chainID, namespace, submissionDuration)
	} else {
		v.metrics.RecordMissingBlock(v.chainID, namespace, blockHeight)
	}
}

// verifyBlock attempts to verify a DA height for a given block status.
func (v *BlockVerifier) verifyBlock(ctx context.Context, header *types.Header) {
	blockHeight := header.Number.Uint64()

	// check if block has transactions
	hasTransactions := header.TxHash != types.EmptyRootHash

	namespace := "header"
	if hasTransactions {
		namespace = "data"
	}

	logger := v.logger.With().Str("namespace", namespace).Uint64("block_height", blockHeight).Logger()
	logger.Info().
		Str("hash", header.Hash().Hex()).
		Time("time", time.Unix(int64(header.Time), 0)).
		Uint64("gas_used", header.GasUsed).
		Bool("has_transactions", hasTransactions).
		Msg("processing block")

	startTime := time.Now()
	blockTime := time.Unix(int64(header.Time), 0)

	// exponential backoff intervals matching observed DA submission timing
	retryIntervals := []time.Duration{
		0, // immediate first attempt
		20 * time.Second,
		40 * time.Second,
		60 * time.Second,
		90 * time.Second,
		120 * time.Second,
	}

	for i, interval := range retryIntervals {
		retries := i + 1

		select {
		case <-ctx.Done():
			logger.Error().Msg("context cancelled")
			return
		case <-time.After(interval):
			// proceed with retry
		}

		blockResult, err := v.evnodeClient.GetBlock(ctx, blockHeight)
		if err != nil {
			logger.Warn().Err(err).Int("attempt", retries).Msg("failed to re-query block from ev-node")
			continue
		}

		daHeight := blockResult.HeaderDaHeight
		if namespace == "data" {
			daHeight = blockResult.DataDaHeight
		}

		if daHeight == 0 {
			logger.Debug().Int("attempt", retries).Msg("block still not submitted to DA, will retry")
			continue
		}

		blockResultWithBlobs, err := v.evnodeClient.GetBlockWithBlobs(ctx, blockHeight)
		if err != nil {
			logger.Warn().Err(err).Int("attempt", retries).Msg("failed to query block from ev-node")
			continue
		}

		daBlockTime, err := v.celestiaClient.GetBlockTimestamp(ctx, daHeight)
		if err != nil {
			logger.Warn().Err(err).Uint64("da_height", daHeight).Msg("failed to get da block timestamp")
			continue
		}

		// the time taken from block time to DA inclusion time.
		submissionDuration := daBlockTime.Sub(blockTime)

		switch namespace {
		case "header":
			verified, err := v.celestiaClient.VerifyBlobAtHeight(ctx, blockResultWithBlobs.HeaderBlob, daHeight, v.headerNS)

			if err != nil {
				logger.Warn().Err(err).Uint64("da_height", daHeight).Msg("verification failed")
				continue
			}

			if verified {
				logger.Info().
					Uint64("da_height", daHeight).
					Dur("duration", time.Since(startTime)).
					Msg("header blob verified on Celestia")
				v.onVerified(namespace, blockHeight, daHeight, true, submissionDuration)
				return
			}

			// verification failed
			if retries >= len(retryIntervals)+1 {
				logger.Error().
					Uint64("da_height", daHeight).
					Dur("duration", time.Since(startTime)).
					Msg("max retries reached - header blob not verified")
				v.onVerified(namespace, blockHeight, daHeight, false, 0)
				return
			}
			logger.Warn().Uint64("da_height", daHeight).Int("attempt", retries).Msg("verification failed, will retry")

		case "data":
			if len(blockResultWithBlobs.DataBlob) == 0 {
				logger.Info().
					Dur("duration", time.Since(startTime)).
					Msg("empty data block - no verification needed")
				v.onVerified(namespace, blockHeight, daHeight, true, submissionDuration)
				return
			}

			// perform actual verification between bytes from ev-node and Celestia.
			verified, err := v.celestiaClient.VerifyDataBlobAtHeight(ctx, blockResultWithBlobs.DataBlob, daHeight, v.dataNS)
			if err != nil {
				logger.Warn().Err(err).Uint64("da_height", daHeight).Msg("verification failed")
				continue
			}

			if verified {
				logger.Info().
					Uint64("da_height", daHeight).
					Dur("duration", time.Since(startTime)).
					Msg("data blob verified on Celestia")
				v.onVerified(namespace, blockHeight, daHeight, true, submissionDuration)
				return
			}

			// verification failed
			if retries >= len(retryIntervals)+1 {
				logger.Error().
					Uint64("da_height", daHeight).
					Dur("duration", time.Since(startTime)).
					Msg("max retries reached - data blob not verified")
				v.onVerified(namespace, blockHeight, daHeight, false, 0)
				return
			}
			logger.Warn().Uint64("da_height", daHeight).Int("attempt", retries).Msg("verification failed, will retry")

		default:
			logger.Error().Str("namespace", namespace).Msg("unknown namespace type")
			return
		}
	}

	// if loop completes without success, log final error
	logger.Error().Msg("max retries exhausted - ALERT: failed to verify block")
	v.onVerified(namespace, blockHeight, 0, false, 0)
}
