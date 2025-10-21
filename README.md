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
- `--celestia-url`: Celestia DA JSON-RPC URL (default: `http://localhost:26658`)
- `--celestia-token`: Celestia authentication token (optional)
- `--duration`: Duration in seconds to stream (0 = infinite )
- `--chain-id`: Chain identifier for metrics labels (default: "testnet")
- `--enable-metrics`: Enable Prometheus metrics HTTP server (default: false)
- `--port`: HTTP server port for metrics (default: 2112)
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
