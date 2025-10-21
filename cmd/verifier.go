package cmd

import (
	"context"
	"github.com/01builders/da-monitor/internal/celestia"
	"github.com/01builders/da-monitor/internal/evm"
	"github.com/01builders/da-monitor/internal/evnode"
	"github.com/01builders/da-monitor/internal/metrics"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"time"
)

// daVerificationStatus tracks a block pending verification
type daVerificationStatus struct {
	blockHeight uint64
	namespace   string // "header" or "data"
}

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

	// internal channels for retry logic
	unverifiedCh chan *daVerificationStatus
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
		unverifiedCh:   make(chan *daVerificationStatus, 100),
	}
}

// VerifyHeadersAndData begins the background retry processor
func (v *BlockVerifier) VerifyHeadersAndData(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
			// when a new unverified status is received, spawn a goroutine to handle retries.
		case status := <-v.unverifiedCh:
			// spawn a goroutine to handle this block's retries
			go v.retryBlock(ctx, status)
		}
	}
}

func (v *BlockVerifier) onVerified(status *daVerificationStatus, verified bool) {
	if verified {
		v.metrics.RemoveVerifiedBlock(v.chainID, status.namespace, status.blockHeight)
	} else {
		v.metrics.RecordMissingBlock(v.chainID, status.namespace, status.blockHeight)
	}
}

func (v *BlockVerifier) ProcessHeaders(ctx context.Context) error {
	headers := make(chan *types.Header, 10)
	sub, err := v.evmClient.SubscribeNewHead(ctx, headers)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	headerCount := 0
	for {
		select {
		case <-ctx.Done():
			v.logger.Info().Int("headers_processed", headerCount).Msg("stream completed")
			return nil
		case header := <-headers:
			headerCount++
			v.enqueueHeader(header)
		}
	}
}

// enqueueHeader queues a block for verification.
func (v *BlockVerifier) enqueueHeader(header *types.Header) {
	blockHeight := header.Number.Uint64()

	// check if block has transactions
	hasTransactions := header.TxHash != types.EmptyRootHash

	v.logger.Info().
		Uint64("block_height", blockHeight).
		Str("hash", header.Hash().Hex()).
		Time("time", time.Unix(int64(header.Time), 0)).
		Uint64("gas_used", header.GasUsed).
		Bool("has_transactions", hasTransactions).
		Msg("processing block")

	// queue header verification
	v.unverifiedCh <- &daVerificationStatus{
		blockHeight: blockHeight,
		namespace:   "header",
	}

	// queue data verification if block has transactions
	if hasTransactions {
		v.unverifiedCh <- &daVerificationStatus{
			blockHeight: blockHeight,
			namespace:   "data",
		}
	}
}

// statusLogger returns a logger pre-populated with status fields
func (v *BlockVerifier) statusLogger(status *daVerificationStatus) zerolog.Logger {
	return v.logger.With().
		Str("namespace", status.namespace).
		Uint64("block_height", status.blockHeight).
		Logger()
}

// retryBlock attempts to re-verify a DA height for a given block status.
func (v *BlockVerifier) retryBlock(ctx context.Context, status *daVerificationStatus) {
	logger := v.statusLogger(status)
	startTime := time.Now()

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

		blockResult, err := v.evnodeClient.GetBlock(ctx, status.blockHeight)
		if err != nil {
			logger.Warn().Err(err).Int("attempt", retries).Msg("failed to re-query block from ev-node")
			continue
		}

		daHeight := blockResult.HeaderDaHeight
		if status.namespace == "data" {
			daHeight = blockResult.DataDaHeight
		}

		if daHeight == 0 {
			logger.Debug().Int("attempt", retries).Msg("block still not submitted to DA, will retry")
			continue
		}

		blockResultWithBlobs, err := v.evnodeClient.GetBlockWithBlobs(ctx, status.blockHeight)
		if err != nil {
			logger.Warn().Err(err).Int("attempt", retries).Msg("failed to re-query block from ev-node")
			continue
		}

		switch status.namespace {
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
				v.onVerified(status, true)
				return
			}

			// verification failed
			if retries >= len(retryIntervals)+1 {
				logger.Error().
					Uint64("da_height", daHeight).
					Dur("duration", time.Since(startTime)).
					Msg("max retries reached - header blob not verified")
				v.onVerified(status, false)
				return
			}
			logger.Warn().Uint64("da_height", daHeight).Int("attempt", retries).Msg("verification failed, will retry")

		case "data":
			if len(blockResultWithBlobs.DataBlob) == 0 {
				logger.Info().
					Dur("duration", time.Since(startTime)).
					Msg("empty data block - no verification needed")
				v.onVerified(status, true)
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
				v.onVerified(status, true)
				return
			}

			// verification failed
			if retries >= len(retryIntervals)+1 {
				logger.Error().
					Uint64("da_height", daHeight).
					Dur("duration", time.Since(startTime)).
					Msg("max retries reached - data blob not verified")
				v.onVerified(status, false)
				return
			}
			logger.Warn().Uint64("da_height", daHeight).Int("attempt", retries).Msg("verification failed, will retry")

		default:
			logger.Error().Str("namespace", status.namespace).Msg("unknown namespace type")
			return
		}
	}

	// if loop completes without success, log final error
	logger.Error().Msg("max retries exhausted - ALERT: failed to verify block")
	v.onVerified(status, false)
}
