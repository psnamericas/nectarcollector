# NectarCollector

A Go service that captures CDR (Call Detail Record) data from multiple sources concurrently, with automatic baud rate detection for serial ports and HTTP POST ingestion support. Data streams to both rotating log files and NATS JetStream.

## Features

- **Multi-Source Capture**: Concurrent capture from serial ports and HTTP POST endpoints
- **Serial Auto-Detection**: Automatic baud rate detection (300-115200) for serial ports
- **HTTP POST Ingestion**: Accept CDR data via HTTP POST requests (for IP-based CDR systems like ECW)
- **Dual Output**: Simultaneous logging to rotating files and NATS JetStream streaming
- **Header Format**: Prepends `[FIPSCODE][A1-16][YYYY-MM-DD HH:MM:SS.mmm]` to each line
- **Health Monitoring**: Periodic health heartbeats to NATS with channel status
- **Event Tracking**: Service lifecycle events (start, stop, reconnect) published to NATS
- **HoneyView Dashboard**: Real-time web dashboard with SSE streaming and JetStream stats
- **Reliable Operation**: Automatic reconnection with exponential backoff, graceful shutdown
- **Configurable**: JSON configuration for all settings

## Architecture

```
Serial Port → Detection (autobaud) → Reader → Line Processing → Header → DualWriter
HTTP POST   → Request Handler      →                          → Header → DualWriter
                                                                            ↓
                                                               Log File + NATS JetStream
```

### Components

- **config/**: Configuration structs, JSON loading, validation
- **serial/**: Reader interface, RealReader implementation, auto-detection algorithms
- **output/**: Header construction, DualWriter (log + NATS), NATS connection, health/event publishers
- **capture/**: Channel (serial), HTTPChannel (HTTP POST), Manager (orchestration)
- **monitoring/**: HoneyView dashboard, REST API, SSE streaming
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
      "side_designation": "A1",
      "enabled": true,
      "description": "Serial feed - auto-detect baud rate"
    },
    {
      "device": "/dev/ttyUSB1",
      "side_designation": "A2",
      "baud_rate": 9600,
      "enabled": true,
      "description": "Serial feed - fixed baud rate"
    },
    {
      "type": "http",
      "path": "/NetworkLogger/Primary/Recorder",
      "listen_port": 8081,
      "side_designation": "B1",
      "enabled": true,
      "description": "HTTP POST endpoint for ECW"
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

## Capture Modes

### Serial Capture (Auto-Detection)

1. Try each baud rate (300, 1200, ..., 115200) sequentially
2. Read data for 5 seconds (configurable)
3. Count printable ASCII characters (0x20-0x7E, plus LF, CR, TAB)
4. Calculate validity ratio = valid_chars / total_bytes
5. Success if ratio ≥ 0.80 AND bytes ≥ 50
6. Lock in baud rate or try next

### HTTP POST Capture

For IP-based CDR systems (e.g., ECW NetworkLogger):
1. Listen on configured port (e.g., 8081)
2. Accept POST requests at configured path
3. Extract body content and prepend header
4. Write to log file and NATS (same as serial)

## Log Files

Per-port rotating log files are written using FIPS code and A-designation format:
```
/var/log/nectarcollector/1429010002-A1.log
/var/log/nectarcollector/1429010002-A2.log
...
```

Rotation is automatic based on size (default 100MB per file, 10 backups).

## NATS Streams

NectarCollector publishes to three JetStream streams:

### CDR Stream
Each channel publishes CDR data to its own subject:
```
{state}.cdr.{vendor}.{fips_code}
# Example: ne.cdr.viper.1314010001
```

### Health Stream
Periodic heartbeats (default 60s) with channel status:
```
{state}.health.{instance_id}
# Example: ne.health.psna-ne-kearney-01
```

### Events Stream
Service lifecycle events (start, stop, reconnect, errors):
```
{state}.events.{instance_id}
# Example: ne.events.psna-ne-kearney-01
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
