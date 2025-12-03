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
	StateStopped
	StateError
)

func (s ChannelState) String() string {
	switch s {
	case StateDetecting:
		return "detecting"
	case StateRunning:
		return "running"
	case StateReconnecting:
		return "reconnecting"
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
	LastLineTime time.Time
	DetectedBaud int
	DetectedFlow bool
	StartTime    time.Time
}

// Channel manages capture from a single serial port
type Channel struct {
	config      *config.PortConfig
	detection   *config.DetectionConfig
	natsConfig  *config.NATSConfig
	recovery    *config.RecoveryConfig
	appConfig   *config.AppConfig
	logConfig   *config.LoggingConfig

	reader     *serial.ReaderWithStats
	dualWriter *output.DualWriter

	state      ChannelState
	stateMutex sync.RWMutex

	stats      ChannelStats
	statsMutex sync.RWMutex

	stopCh chan struct{}
	wg     sync.WaitGroup
	logger *slog.Logger
}

// NewChannel creates a new capture channel
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

	// Get FIPS code (port-specific or app-level)
	fipsCode := portCfg.FIPSCode
	if fipsCode == "" {
		fipsCode = appCfg.FIPSCode
	}

	// Create identifier in format: FIPSCODE-A1 (e.g., 1429010002-A1)
	identifier := fmt.Sprintf("%s-%s", fipsCode, portCfg.ADesignation)

	// Create NATS subject: serial.1429010002-A1
	natsSubject := fmt.Sprintf("%s.%s", natsCfg.SubjectPrefix, identifier)

	// Create dual writer
	dualWriter, err := output.NewDualWriter(&output.DualWriterConfig{
		Device:        portCfg.Device,
		Identifier:    identifier,
		LogBasePath:   logCfg.BasePath,
		LogMaxSizeMB:  logCfg.MaxSizeMB,
		LogMaxBackups: logCfg.MaxBackups,
		LogCompress:   logCfg.Compress,
		NATSConn:      natsConn.Conn(),
		NATSSubject:   natsSubject,
		Logger:        logger,
	})
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

	reader, err := serial.NewRealReader(c.config.Device, baudRate, useFlowControl)
	if err != nil {
		return fmt.Errorf("failed to open port: %w", err)
	}

	c.reader = serial.NewReaderWithStats(reader)

	c.logger.Info("Port opened", "device", c.config.Device, "baud", baudRate, "flow_control", useFlowControl)

	// Phase 3: Read loop
	err = c.readLoop(ctx)
	c.reader.Close()
	c.reader = nil

	return err
}

// readLoop reads lines from the serial port and writes them
func (c *Channel) readLoop(ctx context.Context) error {
	scanner := bufio.NewScanner(c.reader)

	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.stopCh:
			return nil
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					c.reader.IncrementErrors()
					return fmt.Errorf("scanner error: %w", err)
				}
				// EOF - normal termination
				return nil
			}

			line := scanner.Text()
			c.processLine(line)
		}
	}
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

// handleReconnect implements reconnection logic with exponential backoff
func (c *Channel) handleReconnect(ctx context.Context) {
	delay := c.recovery.ReconnectDelay()
	maxDelay := c.recovery.MaxReconnectDelay()
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-time.After(delay):
			attempt++
			c.logger.Info("Reconnection attempt",
				"device", c.config.Device,
				"attempt", attempt,
				"delay", delay)

			// Try to reconnect by returning (will trigger new session)
			return
		}

		// Exponential backoff
		if c.recovery.ExponentialBackoff {
			delay = time.Duration(math.Min(
				float64(delay*2),
				float64(maxDelay),
			))
		}
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
