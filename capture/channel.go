package capture

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"nectarcollector/config"
	"nectarcollector/output"
	"nectarcollector/serial"
)

// ChannelState represents the state of a capture channel
type ChannelState int

const (
	StateDetecting ChannelState = iota
	StateRunning
	StateReconnecting
	StateWaitingForNATS // Paused, waiting for NATS connection
	StateStopped
	StateError
)

// Buffer sizes for line reading - these are generous to handle any line length
// similar to Scannex boxes which handle any data format
const (
	// InitialLineBufferSize is the initial buffer for bufio.Scanner
	InitialLineBufferSize = 64 * 1024 // 64KB

	// MaxLineBufferSize is the maximum line length we'll accept
	// This is intentionally large to handle pathological cases
	MaxLineBufferSize = 1024 * 1024 // 1MB
)

func (s ChannelState) String() string {
	switch s {
	case StateDetecting:
		return "detecting"
	case StateRunning:
		return "running"
	case StateReconnecting:
		return "reconnecting"
	case StateWaitingForNATS:
		return "waiting_for_nats"
	case StateStopped:
		return "stopped"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// ChannelStats tracks statistics for a capture channel
type ChannelStats struct {
	BytesRead    int64
	LinesRead    int64
	Errors       int64
	Reconnects   int64 // Total reconnection attempts
	LastLineTime time.Time
	DetectedBaud int
	DetectedFlow bool
	StartTime    time.Time
}

// NATSChecker provides a way to check NATS connection status
type NATSChecker interface {
	IsConnected() bool
}

// Channel manages capture from a single serial port
type Channel struct {
	config     *config.PortConfig
	detection  *config.DetectionConfig
	natsConfig *config.NATSConfig
	recovery   *config.RecoveryConfig
	appConfig  *config.AppConfig
	logConfig  *config.LoggingConfig

	reader      *serial.ReaderWithStats
	dualWriter  *output.DualWriter
	natsChecker NATSChecker // For checking NATS connection status

	state      ChannelState
	stateMutex sync.RWMutex

	stats               ChannelStats
	consecutiveFailures int64 // For exponential backoff calculation, reset on success
	statsMutex          sync.RWMutex

	stopCh chan struct{}
	wg     sync.WaitGroup
	logger *slog.Logger
}

// NewChannel creates a new capture channel.
// natsConn is required - serial capture is blocked when NATS is unavailable to prevent data loss.
func NewChannel(
	portCfg *config.PortConfig,
	detectionCfg *config.DetectionConfig,
	natsCfg *config.NATSConfig,
	recoveryCfg *config.RecoveryConfig,
	appCfg *config.AppConfig,
	logCfg *config.LoggingConfig,
	natsConn *output.NATSConnection,
	logger *slog.Logger,
) (*Channel, error) {
	if natsConn == nil {
		return nil, fmt.Errorf("NATS connection is required")
	}

	// Get FIPS code (port-specific or app-level)
	fipsCode := portCfg.FIPSCode
	if fipsCode == "" {
		fipsCode = appCfg.FIPSCode
	}

	// Create identifier in format: FIPSCODE-A1 (e.g., 1429010002-A1)
	identifier := fmt.Sprintf("%s-%s", fipsCode, portCfg.ADesignation)

	// Create NATS subject in PEMA format: ne.cdr.intrado.lancaster.3110900001
	// Format: {prefix}.{vendor}.{county}.{fips}
	// Falls back to simpler format if vendor/county not specified
	var natsSubject string
	if portCfg.Vendor != "" && portCfg.County != "" {
		natsSubject = fmt.Sprintf("%s.%s.%s.%s", natsCfg.SubjectPrefix, portCfg.Vendor, portCfg.County, fipsCode)
	} else if portCfg.Vendor != "" {
		natsSubject = fmt.Sprintf("%s.%s.%s", natsCfg.SubjectPrefix, portCfg.Vendor, fipsCode)
	} else {
		natsSubject = fmt.Sprintf("%s.%s", natsCfg.SubjectPrefix, fipsCode)
	}

	// Build dual writer config
	dwConfig := &output.DualWriterConfig{
		Device:        portCfg.Device,
		Identifier:    identifier,
		LogBasePath:   logCfg.BasePath,
		LogMaxSizeMB:  logCfg.MaxSizeMB,
		LogMaxBackups: logCfg.MaxBackups,
		LogCompress:   logCfg.Compress,
		NATSConn:      natsConn.Conn(),
		NATSSubject:   natsSubject,
		Logger:        logger,
	}

	dualWriter, err := output.NewDualWriter(dwConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dual writer: %w", err)
	}

	return &Channel{
		config:      portCfg,
		detection:   detectionCfg,
		natsConfig:  natsCfg,
		recovery:    recoveryCfg,
		appConfig:   appCfg,
		logConfig:   logCfg,
		dualWriter:  dualWriter,
		natsChecker: natsConn,
		state:       StateDetecting,
		stopCh:      make(chan struct{}),
		logger:      logger,
	}, nil
}

// Start begins the capture process
func (c *Channel) Start(ctx context.Context) error {
	c.logger.Info("Starting capture channel", "device", c.config.Device)

	c.statsMutex.Lock()
	c.stats.StartTime = time.Now()
	c.statsMutex.Unlock()

	c.wg.Add(1)
	go c.captureLoop(ctx)

	return nil
}

// Stop stops the capture channel
func (c *Channel) Stop() {
	c.logger.Info("Stopping capture channel", "device", c.config.Device)
	close(c.stopCh)
	c.wg.Wait()

	if c.reader != nil {
		c.reader.Close()
	}

	if c.dualWriter != nil {
		c.dualWriter.Close()
	}

	c.setState(StateStopped)
	c.logger.Info("Capture channel stopped", "device", c.config.Device)
}

// captureLoop is the main loop for the capture channel
func (c *Channel) captureLoop(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			if err := c.runCaptureSession(ctx); err != nil {
				c.logger.Error("Capture session failed", "device", c.config.Device, "error", err)
				c.setState(StateReconnecting)
				c.handleReconnect(ctx)
			}
		}
	}
}

// runCaptureSession runs a single capture session (detect + read)
func (c *Channel) runCaptureSession(ctx context.Context) error {
	// Phase 1: Detection (if needed)
	baudRate := c.config.BaudRate
	useFlowControl := false
	if c.config.UseFlowControl != nil {
		useFlowControl = *c.config.UseFlowControl
	}

	needsDetection := baudRate == 0 || c.config.UseFlowControl == nil

	if needsDetection {
		c.setState(StateDetecting)
		c.logger.Info("Running detection", "device", c.config.Device)

		detector := serial.NewDetector(
			c.config.Device,
			c.detection.BaudRates,
			c.detection.DetectionTimeout(),
			c.detection.MinBytesForValid,
			c.logger,
		)

		result, err := detector.Detect()
		if err != nil {
			c.setState(StateError)
			return fmt.Errorf("detection failed: %w", err)
		}

		baudRate = result.BaudRate
		useFlowControl = result.UseFlowControl

		c.statsMutex.Lock()
		c.stats.DetectedBaud = baudRate
		c.stats.DetectedFlow = useFlowControl
		c.statsMutex.Unlock()

		c.logger.Info("Detection complete",
			"device", c.config.Device,
			"baud", baudRate,
			"flow_control", useFlowControl)
	}

	// Phase 2: Open port
	c.setState(StateRunning)

	// Build serial config from port configuration
	serialConfig := serial.SerialConfig{
		BaudRate:       baudRate,
		DataBits:       c.config.DataBits,
		Parity:         c.config.Parity,
		StopBits:       c.config.StopBits,
		UseFlowControl: useFlowControl,
	}
	reader, err := serial.NewRealReaderWithConfig(c.config.Device, serialConfig)
	if err != nil {
		return fmt.Errorf("failed to open port: %w", err)
	}

	// Use defer immediately after successful open to prevent file descriptor leaks
	// This ensures cleanup even if panic occurs between here and explicit close
	c.reader = serial.NewReaderWithStats(reader)
	defer func() {
		c.reader.Close()
		c.reader = nil
	}()

	c.logger.Info("Port opened", "device", c.config.Device, "baud", baudRate, "flow_control", useFlowControl)

	// Switch to shorter read timeout for production reads
	// This allows faster shutdown response (500ms vs 5s)
	if err := c.reader.SetReadTimeout(serial.DefaultReadTimeout); err != nil {
		c.logger.Warn("Failed to set production read timeout", "device", c.config.Device, "error", err)
		// Non-fatal - continue with detection timeout
	}

	// Reset consecutive failure count on successful connection
	c.statsMutex.Lock()
	c.consecutiveFailures = 0
	c.statsMutex.Unlock()

	// Phase 3: Read loop
	return c.readLoop(ctx)
}

// natsCheckInterval is how often we check NATS status when waiting for reconnection
const natsCheckInterval = 500 * time.Millisecond

// readLoop reads lines from the serial port and writes them.
// CRITICAL: This loop blocks when NATS is disconnected to prevent data loss.
// The sending device's buffer holds data until we're ready to receive again.
func (c *Channel) readLoop(ctx context.Context) error {
	scanner := bufio.NewScanner(c.reader)

	// Increase buffer size for long lines (like Scannex, handle any line length)
	buf := make([]byte, InitialLineBufferSize)
	scanner.Buffer(buf, MaxLineBufferSize)

	for {
		// Check for shutdown signals BEFORE blocking on Scan()
		select {
		case <-ctx.Done():
			return nil
		case <-c.stopCh:
			return nil
		default:
			// Continue
		}

		// Block if NATS is disconnected - don't read serial data we can't deliver
		if !c.waitForNATS(ctx) {
			// Context cancelled or stop requested during wait
			return nil
		}

		if !scanner.Scan() {
			err := scanner.Err()
			if err == nil {
				// EOF - normal termination (port closed, etc.)
				return nil
			}

			// Check if this is a timeout-related error
			// With short read timeouts, the scanner may fail to find a newline
			// before timeout. This is normal - we just check for shutdown and retry.
			// Real errors (port disconnected, etc.) will persist across retries.
			errStr := err.Error()
			if isTimeoutError(errStr) {
				// Timeout is normal with short read timeouts - just loop back
				// and check shutdown signals
				continue
			}

			// Real error - increment counter and return
			c.reader.IncrementErrors()
			return fmt.Errorf("scanner error: %w", err)
		}

		line := scanner.Text()
		c.processLine(line)
	}
}

// waitForNATS blocks until NATS is connected or shutdown is requested.
// Returns true if NATS is connected and we should continue reading.
// Returns false if shutdown was requested and we should exit.
func (c *Channel) waitForNATS(ctx context.Context) bool {
	if c.natsChecker.IsConnected() {
		return true
	}

	// NATS is down - switch to waiting state and pause serial reads
	c.setState(StateWaitingForNATS)
	c.logger.Warn("NATS disconnected, pausing serial reads to prevent data loss",
		"device", c.config.Device)

	ticker := time.NewTicker(natsCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-c.stopCh:
			return false
		case <-ticker.C:
			if c.natsChecker.IsConnected() {
				c.setState(StateRunning)
				c.logger.Info("NATS reconnected, resuming serial reads",
					"device", c.config.Device)
				return true
			}
		}
	}
}

// isTimeoutError checks if an error message indicates a timeout
// rather than a real serial port failure
func isTimeoutError(errMsg string) bool {
	// go.bug.st/serial returns 0 bytes on timeout, which can cause
	// bufio.Scanner to return errors about incomplete tokens or no progress
	return errMsg == "bufio.Scanner: token too long" ||
		errMsg == "unexpected EOF" ||
		// Some platforms may return these
		errMsg == "i/o timeout" ||
		errMsg == "resource temporarily unavailable"
}

// processLine processes a single line from the serial port
func (c *Channel) processLine(line string) {
	// Get FIPS code (port-specific or app-level)
	fipsCode := c.config.FIPSCode
	if fipsCode == "" {
		fipsCode = c.appConfig.FIPSCode
	}

	// Build header
	header := output.BuildHeader(fipsCode, c.config.ADesignation, time.Now().UTC())

	// Write to both log and NATS
	fullLine := header + line
	if err := c.dualWriter.WriteLine(fullLine); err != nil {
		c.logger.Warn("Write error", "device", c.config.Device, "error", err)
		c.reader.IncrementErrors()
	}

	// Update stats
	c.reader.LineRead()

	c.statsMutex.Lock()
	c.stats.LastLineTime = time.Now()
	c.statsMutex.Unlock()
}

// handleReconnect waits before attempting reconnection, using exponential backoff.
// It tracks consecutive failures and increases delay accordingly.
func (c *Channel) handleReconnect(ctx context.Context) {
	c.statsMutex.Lock()
	c.consecutiveFailures++
	c.stats.Reconnects++
	failures := c.consecutiveFailures
	c.statsMutex.Unlock()

	// Calculate delay with exponential backoff based on consecutive failures
	delay := c.recovery.ReconnectDelay()
	if c.recovery.ExponentialBackoff && failures > 1 {
		maxDelay := c.recovery.MaxReconnectDelay()
		// Cap the exponent to avoid overflow with very large failure counts
		exponent := math.Min(float64(failures-1), 30)
		multiplier := math.Pow(2, exponent)
		calculatedDelay := time.Duration(float64(delay) * multiplier)
		if calculatedDelay > maxDelay {
			delay = maxDelay
		} else {
			delay = calculatedDelay
		}
	}

	c.logger.Info("Waiting before reconnection attempt",
		"device", c.config.Device,
		"consecutive_failures", failures,
		"delay", delay)

	select {
	case <-ctx.Done():
		return
	case <-c.stopCh:
		return
	case <-time.After(delay):
		// Delay complete, return to captureLoop which will start a new session
		return
	}
}

// setState updates the channel state
func (c *Channel) setState(state ChannelState) {
	c.stateMutex.Lock()
	c.state = state
	c.stateMutex.Unlock()

	c.logger.Debug("State changed", "device", c.config.Device, "state", state.String())
}

// State returns the current state
func (c *Channel) State() ChannelState {
	c.stateMutex.RLock()
	defer c.stateMutex.RUnlock()
	return c.state
}

// Stats returns current statistics
func (c *Channel) Stats() ChannelStats {
	c.statsMutex.RLock()
	defer c.statsMutex.RUnlock()

	stats := c.stats

	// Get reader stats if available
	if c.reader != nil {
		bytesRead, linesRead, errors := c.reader.Stats()
		stats.BytesRead = bytesRead
		stats.LinesRead = linesRead
		stats.Errors = errors
	}

	return stats
}

// Device returns the device path
func (c *Channel) Device() string {
	return c.config.Device
}
