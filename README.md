# EV Metrics

Data Availability metrics and monitoring tool for evstack using Celestia DA. This tool monitors EVM block headers in real-time, queries ev-node for DA submission information, and verifies blob data on Celestia.

## Features

- Real-time EVM block header streaming
- Automatic verification of header and data blobs on Celestia
- Retry logic with exponential backoff for pending DA submissions
- Prometheus metrics for tracking unverified block ranges
- Support for both streaming and one-shot block verification modes

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/01builders/ev-metrics.git
cd ev-metrics

# Install dependencies
go mod download

# Build the binary
go build -o ev-metrics
```

### Running the Monitor

The monitor command streams EVM block headers and verifies DA submission on Celestia:

```bash
./ev-metrics monitor \
  --header-namespace collect_testnet_header \
  --data-namespace collect_testnet_data
```


### Enable Prometheus Metrics

```bash
./ev-metrics monitor \
  --header-namespace collect_testnet_header \
  --data-namespace collect_testnet_data \
  --enable-metrics \
  --port 2112
```

Metrics will be available at `http://localhost:2112/metrics`

## Configuration

### Command-Line Flags

**Required:**
- `--header-namespace`: Header namespace (e.g. collect_testnet_header )
- `--data-namespace`: Data namespace (e.g. collect_testnet_data )

**Optional:**
- `--evnode-addr`: ev-node Connect RPC address (default: `http://localhost:7331`)
- `--evm-ws-url`: EVM client WebSocket URL (default: `ws://localhost:8546`)
- `--evm-rpc-url`: EVM client JSON-RPC URL for health checks (optional, enables JSON-RPC monitoring)
- `--celestia-url`: Celestia DA JSON-RPC URL (default: `http://localhost:26658`)
- `--celestia-token`: Celestia authentication token (optional)
- `--duration`: Duration in seconds to stream (0 = infinite)
- `--chain-id`: Chain identifier for metrics labels (default: "testnet")
- `--enable-metrics`: Enable Prometheus metrics HTTP server (default: false)
- `--port`: HTTP server port for metrics (default: 2112)
- `--jsonrpc-scrape-interval`: JSON-RPC health check scrape interval in seconds (default: 10)
- `--reference-node`: Reference node RPC endpoint URL (sequencer) for drift monitoring
- `--full-nodes`: Comma-separated list of full node RPC endpoint URLs for drift monitoring
- `--polling-interval`: Polling interval in seconds for checking node block heights (default: 10)
- `--verbose`: Enable verbose logging (default: false)

### Example with Custom Endpoints

```bash
./ev-metrics monitor \
  --header-namespace collect_testnet_header \
  --data-namespace collect_testnet_data \
  --evnode-addr "http://my-evnode:7331" \
  --evm-ws-url "ws://my-evnode:8546" \
  --celestia-url "http://my-celestia:26658"
```

### Example with JSON-RPC Health Monitoring

Enable JSON-RPC request duration monitoring by providing the `--evm-rpc-url` flag:

```bash
./ev-metrics monitor \
  --header-namespace collect_testnet_header \
  --data-namespace collect_testnet_data \
  --evm-ws-url "ws://localhost:8546" \
  --evm-rpc-url "http://localhost:8545" \
  --enable-metrics \
  --jsonrpc-scrape-interval 10
```

This will periodically send `eth_blockNumber` JSON-RPC requests to monitor node health and response times.

## Prometheus Metrics

When metrics are enabled, the following metrics are exposed:

### `ev_metrics_unsubmitted_block_range_start`
- **Type**: Gauge
- **Labels**: `chain_id`, `blob_type`, `range_id`
- **Description**: Start block height of unverified block ranges

### `ev_metrics_unsubmitted_block_range_end`
- **Type**: Gauge
- **Labels**: `chain_id`, `blob_type`, `range_id`
- **Description**: End block height of unverified block ranges

### `ev_metrics_unsubmitted_blocks_total`
- **Type**: Gauge
- **Labels**: `chain_id`, `blob_type`
- **Description**: Total number of unsubmitted blocks

### `ev_metrics_submission_duration_seconds`
- **Type**: Summary
- **Labels**: `chain_id`, `type`
- **Description**: DA blob submission duration from block creation to DA availability

### `ev_metrics_submission_da_height`
- **Type**: Gauge
- **Labels**: `chain_id`, `type`
- **Description**: Latest DA height for header and data submissions

### `ev_metrics_jsonrpc_request_duration_seconds`
- **Type**: Histogram
- **Labels**: `chain_id`
- **Description**: Duration of JSON-RPC requests to the EVM node (enabled when `--evm-rpc-url` is provided)
- **Buckets**: 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0 seconds

### `ev_metrics_jsonrpc_request_slo_seconds`
- **Type**: Gauge
- **Labels**: `chain_id`, `percentile`
- **Description**: SLO thresholds for JSON-RPC request duration (enabled when `--evm-rpc-url` is provided)
- **Values**:
  - `p50`: 0.2s
  - `p90`: 0.35s
  - `p95`: 0.4s
  - `p99`: 0.5s

### Block Height Drift Metrics

When `--reference-node` and `--full-nodes` are provided:

### `ev_metrics_reference_block_height`
- **Type**: Gauge
- **Labels**: `chain_id`, `endpoint`
- **Description**: Current block height of the reference endpoint (sequencer)

### `ev_metrics_target_block_height`
- **Type**: Gauge
- **Labels**: `chain_id`, `endpoint`
- **Description**: Current block height of target endpoints (operator nodes)

### `ev_metrics_block_height_drift`
- **Type**: Gauge
- **Labels**: `chain_id`, `target_endpoint`
- **Description**: Block height difference between reference and target endpoints (positive = target behind, negative = target ahead)
