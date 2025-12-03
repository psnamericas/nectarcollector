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
	device        string
	logWriter     *lumberjack.Logger
	natsConn      *nats.Conn
	natsSubject   string
	logger        *slog.Logger
	natsEnabled   bool
	mu            sync.Mutex
}

// DualWriterConfig contains configuration for DualWriter
type DualWriterConfig struct {
	Device         string
	Identifier     string // FIPS-A format (e.g., "1429010002-A1")
	LogBasePath    string
	LogMaxSizeMB   int
	LogMaxBackups  int
	LogCompress    bool
	NATSConn       *nats.Conn
	NATSSubject    string
	Logger         *slog.Logger
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
