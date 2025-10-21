package cmd

import (
	"context"
	"fmt"
	coreda "github.com/evstack/ev-node/core/da"
	"golang.org/x/sync/errgroup"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/01builders/da-monitor/internal/celestia"
	"github.com/01builders/da-monitor/internal/evm"
	"github.com/01builders/da-monitor/internal/evnode"
	"github.com/01builders/da-monitor/internal/metrics"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

const (
	flagEvNodeAddr        = "evnode-addr"
	flagEvmWSURL          = "evm-ws-url"
	flagCelestiaURL       = "celestia-url"
	flagCelestiaAuthToken = "celestia-token"
	flagHeaderNS          = "header-namespace"
	flagDataNS            = "data-namespace"
	flagDuration          = "duration"
	flagVerbose           = "verbose"
	flagBlockHeight       = "block-height"
	flagPort              = "port"
	flagChain             = "chain-id"
	flagEnableMetrics     = "enable-metrics"

	metricsPath = "/metrics"
)

var flags flagValues

type flagValues struct {
	evnodeAddr        string
	evmWSURL          string
	celestiaURL       string
	celestiaAuthToken string
	headerNS          string
	dataNS            string
	duration          int
	verbose           bool
	blockHeight       int64
	port              int
	chainID           string
	enableMetrics     bool
}

func NewMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor EVM headers and verify corresponding DA data on Celestia",
		Long: `Subscribes to EVM block headers in real-time and for each new block:
1. Gets the blockchain height from the header
2. Queries ev-node Store API to get the DA heights where this block was published
3. Queries Celestia to verify the blobs exist at those DA heights
4. Shows the complete data flow from EVM block → ev-node → Celestia DA`,
		RunE: monitorAndVerifyDataAndHeaders,
	}

	cmd.Flags().StringVar(&flags.evnodeAddr, flagEvNodeAddr, "http://localhost:7331", "ev-node Connect RPC address")
	cmd.Flags().StringVar(&flags.evmWSURL, flagEvmWSURL, "ws://localhost:8546", "EVM client WebSocket URL")
	cmd.Flags().StringVar(&flags.celestiaURL, flagCelestiaURL, "http://localhost:26658", "Celestia DA JSON-RPC URL")
	cmd.Flags().StringVar(&flags.celestiaAuthToken, flagCelestiaAuthToken, "", "Celestia authentication token (optional)")
	cmd.Flags().StringVar(&flags.headerNS, flagHeaderNS, "", "Header namespace (my_app_header_namespace)")
	cmd.Flags().StringVar(&flags.dataNS, flagDataNS, "", "Data namespace (my_app_data_namespace)")
	cmd.Flags().Int64Var(&flags.blockHeight, flagBlockHeight, 0, "Specific block height to verify")
	cmd.Flags().IntVar(&flags.duration, flagDuration, 0, "Duration in seconds to stream (0 = infinite, ignored if block-height is set)")
	cmd.Flags().BoolVar(&flags.verbose, flagVerbose, false, "Enable verbose logging")
	cmd.Flags().BoolVar(&flags.enableMetrics, flagEnableMetrics, false, "Enable Prometheus metrics HTTP server")
	cmd.Flags().IntVar(&flags.port, flagPort, 2112, "HTTP server port for metrics (only used if --enable-metrics is set)")
	cmd.Flags().StringVar(&flags.chainID, flagChain, "testnet", "chainID identifier for metrics labels")

	if err := cmd.MarkFlagRequired(flagHeaderNS); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired(flagDataNS); err != nil {
		panic(err)
	}

	return cmd
}

// initializeClientsAndLoadConfig initializes the clients and loads the configuration based on the provided flags.
func initializeClientsAndLoadConfig(ctx context.Context, logger zerolog.Logger) (*Config, error) {
	headerNS := coreda.NamespaceFromString(flags.headerNS).Bytes()
	logger.Info().Str("header_namespace", fmt.Sprintf("%x", headerNS)).Msg("using header namespace")

	dataNS := coreda.NamespaceFromString(flags.dataNS).Bytes()
	logger.Info().Str("data_namespace", fmt.Sprintf("%x", dataNS)).Msg("using data namespace")

	evNodeClient, evmClient, celestiaClient, err := newClients(ctx, flags, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create clients: %w", err)
	}

	return &Config{
		EvnodeAddr:     flags.evnodeAddr,
		EvmWSURL:       flags.evmWSURL,
		EVNodeClient:   evNodeClient,
		EvmClient:      evmClient,
		CelestiaClient: celestiaClient,
		HeaderNS:       headerNS,
		DataNS:         dataNS,
	}, nil
}

type Config struct {
	EvnodeAddr     string
	EvmWSURL       string
	EVNodeClient   *evnode.Client
	EvmClient      *evm.Client
	CelestiaClient *celestia.Client
	HeaderNS       []byte
	DataNS         []byte
}

func newClients(ctx context.Context, flags flagValues, logger zerolog.Logger) (*evnode.Client, *evm.Client, *celestia.Client, error) {
	evnodeClient := evnode.NewClient(flags.evnodeAddr, logger)

	evmClient, err := evm.NewClient(ctx, flags.evmWSURL, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to EVM client: %w", err)
	}
	celestiaClient, err := celestia.NewClient(ctx, flags.celestiaURL, flags.celestiaAuthToken, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to Celestia: %w", err)
	}
	return evnodeClient, evmClient, celestiaClient, nil
}

func monitorAndVerifyDataAndHeaders(cmd *cobra.Command, args []string) error {
	// Setup logger
	logLevel := zerolog.InfoLevel
	if flags.verbose {
		logLevel = zerolog.DebugLevel
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		Level(logLevel).
		With().
		Timestamp().
		Logger()

	ctx := context.Background()

	cfg, err := initializeClientsAndLoadConfig(ctx, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	defer func() {
		cfg.EvmClient.Close()
		cfg.CelestiaClient.Close()
	}()

	// start HTTP server for metrics if enabled
	if flags.enableMetrics {
		http.Handle(metricsPath, promhttp.Handler())
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})

		serverAddr := fmt.Sprintf(":%d", flags.port)
		go func() {
			logger.Info().
				Str("addr", serverAddr).
				Str("metrics_path", metricsPath).
				Msg("starting HTTP server for Prometheus metrics")
			if err := http.ListenAndServe(serverAddr, nil); err != nil {
				logger.Error().Err(err).Msg("HTTP server failed")
			}
		}()
	} else {
		logger.Info().Msg("Prometheus metrics server disabled (use --enable-metrics to enable)")
	}

	// If specific block height is provided, query it directly
	if flags.blockHeight > 0 {
		// Get header from EVM client
		header, err := cfg.EvmClient.HeaderByNumber(ctx, big.NewInt(flags.blockHeight))
		if err != nil {
			return fmt.Errorf("failed to get header: %w", err)
		}

		verifyBlock(ctx, cfg.EVNodeClient, cfg.CelestiaClient, header, cfg.HeaderNS, cfg.DataNS, logger)
		return nil
	}

	// Setup timeout if specified
	streamCtx := ctx
	if flags.duration > 0 {
		var cancel context.CancelFunc
		streamCtx, cancel = context.WithTimeout(ctx, time.Duration(flags.duration)*time.Second)
		defer cancel()
	}

	// initialize metrics
	m := metrics.New("da_monitor")
	// create block verifier
	verifier := NewBlockVerifier(
		cfg.EVNodeClient,
		cfg.CelestiaClient,
		cfg.EvmClient,
		cfg.HeaderNS,
		cfg.DataNS,
		m,
		flags.chainID,
		logger,
	)

	// start background retry processor
	retryCtx, retryCancel := context.WithCancel(ctx)
	defer retryCancel()

	var g errgroup.Group

	g.Go(func() error {
		return verifier.VerifyHeadersAndData(retryCtx)
	})
	g.Go(func() error {
		return verifier.ProcessHeaders(streamCtx)
	})

	return g.Wait()
}

func verifyBlock(ctx context.Context, evnodeClient *evnode.Client, celestiaClient *celestia.Client, header *types.Header, headerNS, dataNS []byte, logger zerolog.Logger) {
	blockHeight := header.Number.Uint64()

	// check if block has transactions by comparing tx root to EmptyRootHash
	// EmptyRootHash is the Keccak256 hash of the RLP encoding of an empty list
	// this is a deterministic constant value that will always be the same for blocks with no transactions
	hasTransactions := header.TxHash != types.EmptyRootHash

	logger.Info().
		Uint64("block_height", blockHeight).
		Str("hash", header.Hash().Hex()).
		Time("time", time.Unix(int64(header.Time), 0)).
		Uint64("gas_used", header.GasUsed).
		Bool("has_transactions", hasTransactions).
		Msg("processing block")

	// Get block with blob data from ev-node
	blockResult, err := evnodeClient.GetBlockWithBlobs(ctx, blockHeight)
	if err != nil {
		logger.Error().Err(err).Uint64("block_height", blockHeight).Msg("failed to get block with blobs from ev-node")
		return
	}

	logger.Info().
		Uint64("header_da_height", blockResult.HeaderDaHeight).
		Uint64("data_da_height", blockResult.DataDaHeight).
		Int("header_blob_size", len(blockResult.HeaderBlob)).
		Int("data_blob_size", len(blockResult.DataBlob)).
		Msg("retrieved block data from ev-node")

	// Verify header blob exists on Celestia
	logger.Debug().Msg("verifying header blob with commitment...")
	headerExists, err := celestiaClient.VerifyBlobAtHeight(ctx, blockResult.HeaderBlob, blockResult.HeaderDaHeight, headerNS)
	if err != nil {
		logger.Error().Err(err).Uint64("da_height", blockResult.HeaderDaHeight).Msg("failed to verify header blob")
	} else if !headerExists {
		logger.Error().
			Uint64("da_height", blockResult.HeaderDaHeight).
			Msg("ALERT: header blob NOT FOUND on Celestia - commitment does not match any blob at this height")
	} else {
		logger.Info().
			Uint64("da_height", blockResult.HeaderDaHeight).
			Msg("✓ header blob VERIFIED on Celestia - commitment matches")
	}

	// only verify data blob if block has transactions
	if hasTransactions {
		logger.Info().Msg("verifying data blob (accounting for SignedData wrapper)...")
		dataExists, err := celestiaClient.VerifyDataBlobAtHeight(ctx, blockResult.DataBlob, blockResult.DataDaHeight, dataNS)
		if err != nil {
			logger.Error().Err(err).Uint64("da_height", blockResult.DataDaHeight).Msg("failed to verify data blob")
		} else if !dataExists {
			logger.Error().
				Uint64("da_height", blockResult.DataDaHeight).
				Msg("data blob NOT FOUND on Celestia")
		} else {
			logger.Info().
				Uint64("da_height", blockResult.DataDaHeight).
				Msg("data blob VERIFIED on Celestia")
		}
	} else {
		logger.Debug().Msg("skipped data verification for empty block")
	}
}
