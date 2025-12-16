package output

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"gopkg.in/natefinch/lumberjack.v2"
)

// DualWriter writes data to both a rotating log file and NATS JetStream
type DualWriter struct {
	device      string
	logWriter   *lumberjack.Logger
	natsConn    *NATSConnection
	natsSubject string
	logger      *slog.Logger
	natsEnabled bool
	mu          sync.Mutex
}

// DualWriterConfig contains configuration for DualWriter
type DualWriterConfig struct {
	Device        string
	Identifier    string // FIPS-A format (e.g., "1429010002-A1")
	LogBasePath   string
	LogMaxSizeMB  int
	LogMaxBackups int
	LogCompress   bool
	NATSConn      *NATSConnection
	NATSSubject   string
	Logger        *slog.Logger
}

// NewDualWriter creates a new DualWriter
func NewDualWriter(cfg *DualWriterConfig) (*DualWriter, error) {
	// Create log file path from identifier
	// e.g., 1429010002-A1 -> /var/log/nectarcollector/1429010002-A1.log
	logPath := filepath.Join(cfg.LogBasePath, cfg.Identifier+".log")

	// Create rotating log writer
	logWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		Compress:   cfg.LogCompress,
	}

	dw := &DualWriter{
		device:      cfg.Device,
		logWriter:   logWriter,
		natsConn:    cfg.NATSConn,
		natsSubject: cfg.NATSSubject,
		logger:      cfg.Logger,
		natsEnabled: cfg.NATSConn != nil,
	}

	cfg.Logger.Info("Initialized dual writer",
		"device", cfg.Device,
		"log_path", logPath,
		"nats_subject", cfg.NATSSubject,
		"nats_enabled", dw.natsEnabled)

	return dw, nil
}

// Write writes data to both log file and NATS
func (dw *DualWriter) Write(data string) error {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	var lastErr error

	// Write to log file (primary output)
	if _, err := io.WriteString(dw.logWriter, data); err != nil {
		dw.logger.Error("Failed to write to log file",
			"device", dw.device,
			"error", err)
		lastErr = err
	}

	// Write to NATS (secondary output - continue on failure)
	if dw.natsEnabled {
		if err := dw.natsConn.Publish(dw.natsSubject, []byte(data)); err != nil {
			dw.logger.Warn("Failed to publish to NATS",
				"device", dw.device,
				"subject", dw.natsSubject,
				"error", err)
			// Don't override lastErr if log write succeeded
			if lastErr == nil {
				lastErr = err
			}
		}
	}

	return lastErr
}

// WriteLine writes a single line (adds newline if not present)
func (dw *DualWriter) WriteLine(line string) error {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	return dw.Write(line)
}

// Close closes the log writer
func (dw *DualWriter) Close() error {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	if dw.logWriter != nil {
		return dw.logWriter.Close()
	}

	return nil
}

// NATSConnection manages NATS connection
type NATSConnection struct {
	conn   *nats.Conn
	url    string
	logger *slog.Logger
	mu     sync.RWMutex
}

// NewNATSConnection creates a new NATS connection
func NewNATSConnection(url string, maxReconnects int, logger *slog.Logger) (*NATSConnection, error) {
	opts := []nats.Option{
		nats.MaxReconnects(maxReconnects),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("Reconnected to NATS", "url", nc.ConnectedUrl())
		}),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				logger.Warn("Disconnected from NATS", "error", err)
			}
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			logger.Info("NATS connection closed")
		}),
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", url, err)
	}

	logger.Info("Connected to NATS", "url", url)

	return &NATSConnection{
		conn:   conn,
		url:    url,
		logger: logger,
	}, nil
}

// Close closes the NATS connection
func (nc *NATSConnection) Close() {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	if nc.conn != nil {
		nc.conn.Close()
		nc.conn = nil
		nc.logger.Info("Closed NATS connection")
	}
}

// Conn returns the underlying NATS connection
func (nc *NATSConnection) Conn() *nats.Conn {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	return nc.conn
}

// IsConnected returns true if connected to NATS
func (nc *NATSConnection) IsConnected() bool {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	return nc.conn != nil && nc.conn.IsConnected()
}

// JetStream returns a JetStream context for the connection
func (nc *NATSConnection) JetStream() (nats.JetStreamContext, error) {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	if nc.conn == nil {
		return nil, fmt.Errorf("NATS connection is nil")
	}
	return nc.conn.JetStream()
}

// Publish sends a message to NATS
func (nc *NATSConnection) Publish(subject string, data []byte) error {
	nc.mu.RLock()
	conn := nc.conn
	nc.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("NATS connection is nil")
	}
	return conn.Publish(subject, data)
}

// NATSStats contains NATS connection statistics
type NATSStats struct {
	Connected    bool   `json:"connected"`
	URL          string `json:"url"`
	ConnectedURL string `json:"connected_url,omitempty"`
	ServerID     string `json:"server_id,omitempty"`
	Reconnects   uint64 `json:"reconnects"`
	// Stream stats (from JetStream)
	Streams map[string]StreamStats `json:"streams,omitempty"`
}

// StreamStats contains stats for a single JetStream stream
type StreamStats struct {
	Messages uint64 `json:"messages"`
	Bytes    uint64 `json:"bytes"`
}

// Stats returns NATS connection statistics
func (nc *NATSConnection) Stats() NATSStats {
	nc.mu.RLock()
	defer nc.mu.RUnlock()

	stats := NATSStats{
		URL: nc.url,
	}

	if nc.conn == nil {
		return stats
	}

	stats.Connected = nc.conn.IsConnected()
	if stats.Connected {
		stats.ConnectedURL = nc.conn.ConnectedUrl()
		stats.ServerID = nc.conn.ConnectedServerId()
	}

	// Get reconnect count from NATS client
	natsStats := nc.conn.Stats()
	stats.Reconnects = natsStats.Reconnects

	return stats
}

// StatsWithStreams returns NATS stats including JetStream stream info
func (nc *NATSConnection) StatsWithStreams(streamNames []string) NATSStats {
	stats := nc.Stats()

	if !stats.Connected || len(streamNames) == 0 {
		return stats
	}

	js, err := nc.conn.JetStream()
	if err != nil {
		return stats
	}

	stats.Streams = make(map[string]StreamStats)
	for _, name := range streamNames {
		info, err := js.StreamInfo(name)
		if err != nil {
			continue
		}
		stats.Streams[name] = StreamStats{
			Messages: info.State.Msgs,
			Bytes:    info.State.Bytes,
		}
	}

	return stats
}
