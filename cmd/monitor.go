package cmd

import (
	"context"
	"fmt"
	coreda "github.com/evstack/ev-node/core/da"
	"golang.org/x/sync/errgroup"
	"net/http"
	"os"
	"time"

	"github.com/01builders/da-monitor/internal/celestia"
	"github.com/01builders/da-monitor/internal/evm"
	"github.com/01builders/da-monitor/internal/evnode"
	"github.com/01builders/da-monitor/internal/metrics"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"strings"
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
	flagPort              = "port"
	flagChain             = "chain-id"
	flagEnableMetrics     = "enable-metrics"
	flagReferenceNode     = "reference-node"
	flagFullNodes         = "full-nodes"
	flagPollingInterval   = "polling-interval"

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
	port              int
	chainID           string
	enableMetrics     bool
	referenceNode     string
	fullNodes         string
	pollingInterval   int
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
	cmd.Flags().IntVar(&flags.duration, flagDuration, 0, "Duration in seconds to stream (0 = infinite)")
	cmd.Flags().BoolVar(&flags.verbose, flagVerbose, false, "Enable verbose logging")
	cmd.Flags().BoolVar(&flags.enableMetrics, flagEnableMetrics, false, "Enable Prometheus metrics HTTP server")
	cmd.Flags().IntVar(&flags.port, flagPort, 2112, "HTTP server port for metrics (only used if --enable-metrics is set)")
	cmd.Flags().StringVar(&flags.chainID, flagChain, "testnet", "chainID identifier for metrics labels")
	cmd.Flags().StringVar(&flags.referenceNode, flagReferenceNode, "", "Reference node RPC endpoint URL (sequencer) for drift monitoring")
	cmd.Flags().StringVar(&flags.fullNodes, flagFullNodes, "", "Comma-separated list of full node RPC endpoint URLs for drift monitoring")
	cmd.Flags().IntVar(&flags.pollingInterval, flagPollingInterval, 10, "Polling interval in seconds for checking node block heights (default: 10)")

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

// monitorNodeDrift polls the reference and full nodes at the specified interval
// and emits metrics for their block heights and drift
func monitorNodeDrift(ctx context.Context, m *metrics.Metrics, referenceNode string, fullNodes []string, pollingInterval int, logger zerolog.Logger) error {
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

			m.RecordReferenceBlockHeight(flags.chainID, referenceNode, refHeight)
			logger.Info().Uint64("height", refHeight).Str("endpoint", referenceNode).Msg("recorded reference node height")

			// get each full node height and calculate drift
			for _, fullNode := range fullNodes {
				targetHeight, err := getBlockHeight(ctx, fullNode)
				if err != nil {
					logger.Error().Err(err).Str("endpoint", fullNode).Msg("failed to get full node block height")
					continue
				}

				m.RecordTargetBlockHeight(flags.chainID, fullNode, targetHeight)
				m.RecordBlockHeightDrift(flags.chainID, fullNode, refHeight, targetHeight)

				drift := int64(refHeight) - int64(targetHeight)
				logger.Info().
					Uint64("ref_height", refHeight).
					Uint64("target_height", targetHeight).
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

	// start node drift monitoring if configured
	if flags.referenceNode != "" && flags.fullNodes != "" {
		fullNodeList := strings.Split(flags.fullNodes, ",")
		// trim whitespace from each endpoint
		for i := range fullNodeList {
			fullNodeList[i] = strings.TrimSpace(fullNodeList[i])
		}

		g.Go(func() error {
			return monitorNodeDrift(retryCtx, m, flags.referenceNode, fullNodeList, flags.pollingInterval, logger)
		})
	} else {
		logger.Info().Msg("node drift monitoring disabled (set --reference-node and --full-nodes to enable)")
	}

	return g.Wait()
}
