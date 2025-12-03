# NectarCollector

A Go service that captures serial data from multiple ports concurrently, with automatic baud rate and pinout detection, streaming to both rotating log files and NATS JetStream.

## Features

- **Multi-Port Capture**: Concurrent capture from multiple serial ports (one goroutine per port)
- **Auto-Detection**: Automatic baud rate detection (300-115200) and pinout resolution (null modem vs straight-through)
- **Dual Output**: Simultaneous logging to rotating files and NATS JetStream streaming
- **Header Format**: Prepends `[FIPSCODE][A1-16][YYYY-MM-DD HH:MM:SS.mmm]` to each line
- **Reliable Operation**: Automatic reconnection with exponential backoff, graceful shutdown
- **Configurable**: JSON configuration for all settings

## Architecture

```
Serial Port → Detection (autobaud + pinout) → Reader
    → Line-by-line processing → Header construction
    → Dual Writer (log file + NATS JetStream)
```

### Components

- **config/**: Configuration structs, JSON loading, validation
- **serial/**: Reader interface, RealReader implementation, auto-detection algorithms
- **output/**: Header construction, DualWriter (log + NATS), NATS connection management
- **capture/**: Channel (per-port state machine), Manager (orchestration)
- **main.go**: Entry point, signal handling, graceful shutdown

## Installation

```bash
# Build from source
go build -o nectarcollector

# Or install directly
go install
```

## Configuration

Create a configuration file (see `configs/example-config.json`):

```json
{
  "app": {
    "name": "NectarCollector",
    "instance_id": "datacenter-01",
    "fips_code": "1429010002"
  },
  "ports": [
    {
      "device": "/dev/ttyUSB0",
      "a_designation": "A1",
      "enabled": true,
      "description": "Primary feed - auto-detect all"
    },
    {
      "device": "/dev/ttyUSB1",
      "a_designation": "A2",
      "baud_rate": 9600,
      "use_flow_control": false,
      "enabled": true,
      "description": "Secondary - manual config, skip detection"
    }
  ],
  "detection": {
    "baud_rates": [300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200],
    "detection_timeout_sec": 5,
    "min_bytes_for_valid": 50
  },
  "nats": {
    "url": "nats://localhost:4222",
    "subject_prefix": "serial",
    "max_reconnects": 10,
    "reconnect_wait_sec": 5
  },
  "logging": {
    "base_path": "/var/log/nectarcollector",
    "max_size_mb": 100,
    "max_backups": 10,
    "compress": true,
    "level": "info"
  },
  "monitoring": {
    "port": 8080
  },
  "recovery": {
    "reconnect_delay_sec": 5,
    "max_reconnect_delay_sec": 300,
    "exponential_backoff": true
  }
}
```

## Usage

```bash
# Run with configuration file
./nectarcollector -config configs/example-config.json

# Enable debug logging
./nectarcollector -config configs/example-config.json -debug

# Show version
./nectarcollector -version
```

## Output Format

Each captured line is prepended with a header:

```
[1429010002][A1][2025-12-03 15:04:05.123] <original line data>
```

Where:
- `1429010002` = FIPS code (configurable per app or per port)
- `A1` = A designation (A1-A16, configurable per port)
- `2025-12-03 15:04:05.123` = UTC timestamp with milliseconds

## Auto-Detection Algorithms

### Autobaud Detection

1. Try each baud rate (300, 1200, ..., 115200) sequentially
2. Read data for 5 seconds (configurable)
3. Count printable ASCII characters (0x20-0x7E, plus LF, CR, TAB)
4. Calculate validity ratio = valid_chars / total_bytes
5. Success if ratio ≥ 0.80 AND bytes ≥ 50
6. Lock in baud rate or try next

### Pinout Detection

1. Test with RTS/CTS flow control enabled
2. If data received → straight-through cable detected
3. Otherwise, test without flow control
4. If data received → null modem cable detected
5. Fail if no data with either setting

## Log Files

Per-port rotating log files are written using FIPS code and A-designation format:
```
/var/log/nectarcollector/1429010002-A1.log
/var/log/nectarcollector/1429010002-A2.log
...
```

Rotation is automatic based on size (default 100MB per file, 10 backups).

## NATS Topics

Each serial port publishes to its own NATS JetStream subject using FIPS-A format:
```
serial.1429010002-A1
serial.1429010002-A2
...
```

## Error Handling

- **Serial failures**: Automatic reconnection with exponential backoff
- **NATS failures**: Log warning, continue to file (NATS is secondary)
- **Detection failures**: Skip port, log error, don't crash service
- **File system failures**: Fatal on startup (can't operate without logs)

## Dependencies

- `go.bug.st/serial` v1.6.1 - Cross-platform serial port support
- `github.com/nats-io/nats.go` v1.31.0 - NATS client with JetStream
- `gopkg.in/natefinch/lumberjack.v2` v2.2.1 - Log file rotation

## License

Proprietary - All rights reserved
