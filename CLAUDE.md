# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Build
go build -o nectarcollector

# Run with configuration
./nectarcollector -config configs/example-config.json

# Run with debug logging
./nectarcollector -config configs/example-config.json -debug

# Show version
./nectarcollector -version

# Run tests (none currently exist)
go test ./...
```

## Architecture Overview

NectarCollector is a Go service that captures serial data from multiple ports concurrently, with automatic baud rate detection, streaming to rotating log files and NATS JetStream.

### Core Data Flow
```
Serial Port → Detection (autobaud) → Reader → Line Processing → Header Construction → DualWriter (log + NATS)
```

### Package Structure

- **main.go**: Entry point, signal handling (SIGINT/SIGTERM), graceful 30-second shutdown, slog-based logging setup
- **config/**: JSON configuration loading with defaults and validation. `Config` struct contains nested configs for app, ports, detection, NATS, logging, monitoring, and recovery
- **serial/**: Serial port abstraction with `Reader` interface, `RealReader` implementation using go.bug.st/serial, and `Detector` for autobaud
- **output/**: `DualWriter` writes to both lumberjack rotating logs and NATS. `NATSConnection` manages NATS client with reconnection handlers
- **capture/**: `Channel` is a per-port state machine (StateDetecting→StateRunning→StateReconnecting). `Manager` orchestrates all channels, manages shared NATS connection
- **monitoring/**: HTTP server with embedded `dashboard.html` (HoneyView). Endpoints: `/` (dashboard), `/api/health`, `/api/stats`, `/api/feed`

### Key Patterns

- **Identifier Format**: `{FIPS_CODE}-{A_DESIGNATION}` (e.g., `1429010002-A1`) used for log filenames and NATS subjects
- **Header Format**: `[FIPSCODE][A1-16][YYYY-MM-DD HH:MM:SS.mmm]` prepended to each captured line
- **Autobaud Detection**: Iterates baud rates 300-115200, reads for timeout period, calculates printable ASCII ratio (≥0.80 with ≥50 bytes = success)
- **Error Handling**: Serial failures trigger exponential backoff reconnection; NATS failures logged but don't block capture; detection failures skip port without crashing

### Dependencies

- `go.bug.st/serial` - Cross-platform serial port
- `github.com/nats-io/nats.go` - NATS client with JetStream
- `gopkg.in/natefinch/lumberjack.v2` - Log rotation
