# DA Monitor

Data Availability monitoring tool for rollups using Celestia DA. This tool monitors EVM block headers in real-time, queries ev-node for DA submission information, and verifies blob data on Celestia.

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
git clone https://github.com/01builders/da-monitor.git
cd da-monitor

# Install dependencies
go mod download

# Build the binary
go build -o da-monitor
```

### Running the Monitor

The monitor command streams EVM block headers and verifies DA submission on Celestia:

```bash
./da-monitor monitor \
  --header-namespace "0x0000000000000000000000000000000000000086c8d75ed85ef620ef51" \
  --data-namespace "0x00000000000000000000000000000000000000893f79d6e2c81a4d08c4"
```

### Verify a Specific Block

To verify a single block height instead of streaming:

```bash
./da-monitor monitor \
  --header-namespace "0x0000000000000000000000000000000000000086c8d75ed85ef620ef51" \
  --data-namespace "0x00000000000000000000000000000000000000893f79d6e2c81a4d08c4" \
  --block-height 100
```

### Enable Prometheus Metrics

```bash
./da-monitor monitor \
  --header-namespace "0x0000000000000000000000000000000000000086c8d75ed85ef620ef51" \
  --data-namespace "0x00000000000000000000000000000000000000893f79d6e2c81a4d08c4" \
  --enable-metrics \
  --port 2112
```

Metrics will be available at `http://localhost:2112/metrics`

## Configuration

### Command-Line Flags

**Required:**
- `--header-namespace`: Header namespace (29-byte hex string)
- `--data-namespace`: Data namespace (29-byte hex string)

**Optional:**
- `--evnode-addr`: ev-node Connect RPC address (default: `http://localhost:7331`)
- `--evm-ws-url`: EVM client WebSocket URL (default: `ws://localhost:8546`)
- `--celestia-url`: Celestia DA JSON-RPC URL (default: `http://localhost:26658`)
- `--celestia-token`: Celestia authentication token (optional)
- `--block-height`: Specific block height to verify (0 = stream mode, default: 0)
- `--duration`: Duration in seconds to stream (0 = infinite, default: 30)
- `--chainID`: Chain identifier for metrics labels (default: "testnet")
- `--enable-metrics`: Enable Prometheus metrics HTTP server (default: false)
- `--port`: HTTP server port for metrics (default: 2112)
- `--verbose`: Enable verbose logging (default: false)

### Example with Custom Endpoints

```bash
./da-monitor monitor \
  --header-namespace "0x0000000000000000000000000000000000000086c8d75ed85ef620ef51" \
  --data-namespace "0x00000000000000000000000000000000000000893f79d6e2c81a4d08c4" \
  --evnode-addr "http://my-evnode:7331" \
  --evm-ws-url "ws://my-evm:8546" \
  --celestia-url "http://my-celestia:26658" \
  --duration 0 \
  --verbose
```

## How It Works

### Block Verification Flow

For each new block header:

1. **Header Reception**: Subscribes to EVM block headers via WebSocket
2. **DA Query**: Queries ev-node Store API for DA heights where block was published
3. **Verification**: Verifies header and data blobs on Celestia at those DA heights
4. **Retry Logic**: Automatically retries with exponential backoff if DA submission is pending
5. **Metrics**: Updates Prometheus metrics tracking verification status

### Retry Strategy

The monitor uses intelligent retry logic for pending DA submissions:
- **Immediate**: First attempt right away
- **20s**: Second attempt after 20 seconds
- **40s**: Third attempt after 40 seconds
- **60s**: Fourth attempt after 60 seconds
- **90s**: Fifth attempt after 90 seconds
- **120s**: Sixth and final attempt after 120 seconds

After max retries, unverified blocks are recorded in Prometheus metrics.

## Prometheus Metrics

When metrics are enabled, the following metrics are exposed:

### `da_monitor_unsubmitted_block_range_start`
- **Type**: Gauge
- **Labels**: `chain`, `blob_type`, `range_id`
- **Description**: Start block height of unverified block ranges

### `da_monitor_unsubmitted_block_range_end`
- **Type**: Gauge
- **Labels**: `chain`, `blob_type`, `range_id`
- **Description**: End block height of unverified block ranges

### Example Metrics

```
# HELP da_monitor_unsubmitted_block_range_start start of unsubmitted block range
# TYPE da_monitor_unsubmitted_block_range_start gauge
da_monitor_unsubmitted_block_range_start{blob_type="header",chain="testnet",range_id="100-105"} 100

# HELP da_monitor_unsubmitted_block_range_end end of unsubmitted block range
# TYPE da_monitor_unsubmitted_block_range_end gauge
da_monitor_unsubmitted_block_range_end{blob_type="header",chain="testnet",range_id="100-105"} 105
```

## Example Output

### Streaming Mode

```
2024-01-15T10:30:45Z INF processing block block_height=150 hash=0x1234... has_transactions=true gas_used=21000
2024-01-15T10:30:45Z INF header blob verified on Celestia block_height=150 namespace=header da_height=8423260 duration=45ms
2024-01-15T10:30:45Z INF data blob verified on Celestia block_height=150 namespace=data da_height=8423260 duration=67ms
```

### One-Shot Mode

```
2024-01-15T10:30:45Z INF using header namespace header_namespace=0000000000000000000000000000000000000086c8d75ed85ef620ef51
2024-01-15T10:30:45Z INF using data namespace data_namespace=00000000000000000000000000000000000000893f79d6e2c81a4d08c4
2024-01-15T10:30:45Z INF processing block block_height=100 hash=0x5678... has_transactions=true gas_used=42000
2024-01-15T10:30:45Z INF retrieved block data from ev-node header_da_height=8423100 data_da_height=8423100
2024-01-15T10:30:45Z INF ✓ header blob VERIFIED on Celestia - commitment matches da_height=8423100
2024-01-15T10:30:45Z INF data blob VERIFIED on Celestia da_height=8423100
```

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...
```

### Building Docker Image

```bash
docker build -t da-monitor .
```

## Architecture

### Components

- **cmd/monitor.go**: CLI entry point and orchestration
- **cmd/verifier.go**: Block verification and retry logic
- **internal/celestia**: Celestia DA client
- **internal/evm**: EVM client for header streaming
- **internal/evnode**: ev-node Store API client
- **internal/metrics**: Prometheus metrics tracking

### Key Types

```go
// BlockVerifier handles verification of blocks against Celestia DA
type BlockVerifier struct {
    evnodeClient   *evnode.Client
    celestiaClient *celestia.Client
    evmClient      *evm.Client
    headerNS       []byte
    dataNS         []byte
    metrics        *metrics.Metrics
    chainID        string
}
```

## Troubleshooting

### Connection Issues

If you see connection errors, verify your endpoints are correct:

```bash
# Test ev-node connectivity
curl http://localhost:7331

# Test Celestia connectivity
curl http://localhost:26658
```

### Missing Blobs

If blobs are not found on Celestia:
- Check that the namespaces are correct
- Verify DA submission is enabled on your ev-node
- Check ev-node logs for DA submission errors
- Wait for retry logic to complete (up to 5 minutes)

### Prometheus Metrics Not Available

Ensure you've enabled metrics:
```bash
./da-monitor monitor ... --enable-metrics --port 2112
```

Then check: `http://localhost:2112/metrics`

## License

See LICENSE file for details.
