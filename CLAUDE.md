# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Build
go build -o nectarcollector

# Build for Linux deployment
GOOS=linux GOARCH=amd64 go build -o nectarcollector-linux

# Run with configuration
./nectarcollector -config configs/example-config.json

# Run with debug logging
./nectarcollector -config configs/example-config.json -debug

# Show version
./nectarcollector -version

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...
```

## Architecture Overview

NectarCollector is a Go service that captures CDR data from serial ports and HTTP POST endpoints, with automatic baud rate detection for serial, streaming to rotating log files and NATS JetStream.

### Core Data Flow
```
Serial Port → Detection (autobaud) → Reader → Line Processing → Header → DualWriter
HTTP POST   → Request Handler      →                          → Header → DualWriter
                                                                            ↓
                                                               Log File + NATS JetStream
```

### Package Structure

- **main.go**: Entry point, signal handling (SIGINT/SIGTERM), graceful shutdown, slog-based logging setup
- **config/**: JSON configuration loading with defaults and validation. `Config` struct contains nested configs for app, ports, detection, NATS, logging, monitoring, and recovery. Supports both serial and HTTP port types.
- **serial/**: Serial port abstraction with `Reader` interface, `RealReader` implementation using go.bug.st/serial, and `Detector` for autobaud
- **output/**: `DualWriter` writes to both lumberjack rotating logs and NATS. `NATSConnection` manages NATS client with reconnection handlers. `HealthPublisher` sends periodic heartbeats. `EventPublisher` tracks service lifecycle events.
- **capture/**: `Channel` is a per-serial-port state machine (StateDetecting→StateRunning→StateReconnecting). `HTTPChannel` handles HTTP POST ingestion. `Manager` orchestrates all channels, manages shared NATS connection
- **monitoring/**: HTTP server with embedded `dashboard.html` (HoneyView). Endpoints: `/` (dashboard), `/api/health`, `/api/stats`, `/api/feed`, `/api/stream` (SSE), `/api/events`, `/api/ports`, `/api/system`

### Key Patterns

- **Identifier Format**: `{FIPS_CODE}-{A_DESIGNATION}` (e.g., `1429010002-A1`) used for log filenames
- **Header Format**: `[FIPSCODE][A1-16][YYYY-MM-DD HH:MM:SS.mmm]` prepended to each captured line
- **NATS Subjects**: `{state}.cdr.{vendor}.{fips}` for CDR, `{state}.health.{instance}` for health, `{state}.events.{instance}` for events
- **Autobaud Detection**: Iterates baud rates 300-115200, reads for timeout period, calculates printable ASCII ratio (≥0.80 with ≥50 bytes = success)
- **HTTP Capture**: Accepts POST requests on configured path, extracts body, prepends header, writes to dual output
- **Error Handling**: Serial failures trigger exponential backoff reconnection; NATS failures logged but don't block capture; detection failures skip port without crashing

### Dependencies

- `go.bug.st/serial` - Cross-platform serial port
- `github.com/nats-io/nats.go` - NATS client with JetStream
- `gopkg.in/natefinch/lumberjack.v2` - Log rotation
