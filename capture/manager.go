package capture

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"nectarcollector/config"
	"nectarcollector/output"
)

// Manager manages multiple capture channels (serial and HTTP)
type Manager struct {
	config          *config.Config
	channels        []*Channel     // Serial channels
	httpChannels    []*HTTPChannel // HTTP channels
	natsConn        *output.NATSConnection
	healthPublisher *output.HealthPublisher
	eventPublisher  *output.EventPublisher
	logger          *slog.Logger
	mu              sync.RWMutex
}

// NewManager creates a new capture manager
func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		config:       cfg,
		channels:     make([]*Channel, 0),
		httpChannels: make([]*HTTPChannel, 0),
		logger:       logger,
	}
}

// Start initializes and starts all enabled capture channels.
// NATS connection is required - returns error if NATS is unavailable.
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting capture manager", "instance", m.config.App.InstanceID)

	// Connect to NATS - required for operation
	natsConn, err := output.NewNATSConnection(
		m.config.NATS.URL,
		m.config.NATS.MaxReconnects,
		m.logger,
	)
	if err != nil {
		return fmt.Errorf("NATS connection required: %w", err)
	}
	m.natsConn = natsConn

	// Create event publisher (optional - nil-safe if NATS fails later)
	eventsSubject := output.BuildEventsSubject(m.config.NATS.SubjectPrefix, m.config.App.InstanceID)
	m.eventPublisher = output.NewEventPublisher(&output.EventPublisherConfig{
		Conn:       m.natsConn,
		Subject:    eventsSubject,
		InstanceID: m.config.App.InstanceID,
		Logger:     m.logger,
	})

	// Check if previous run ended cleanly (power loss, crash, reboot detection)
	m.eventPublisher.CheckAndPublishUncleanShutdown()

	// Publish service start event
	m.eventPublisher.PublishServiceStart("1.0.0")

	// Create and start channels for enabled ports
	startedCount := 0
	for _, portCfg := range m.config.Ports {
		if !portCfg.Enabled {
			portID := portCfg.Device
			if portCfg.IsHTTP() {
				portID = portCfg.Path
			}
			m.logger.Info("Skipping disabled port", "port", portID)
			continue
		}

		if portCfg.IsHTTP() {
			// Create HTTP channel (will be registered with HTTP server later)
			httpChannel, err := m.createHTTPChannel(portCfg)
			if err != nil {
				m.logger.Error("Failed to create HTTP channel", "path", portCfg.Path, "error", err)
				continue
			}

			m.mu.Lock()
			m.httpChannels = append(m.httpChannels, httpChannel)
			m.mu.Unlock()

			startedCount++
			m.logger.Info("Created HTTP capture channel",
				"path", portCfg.Path,
				"a_designation", portCfg.ADesignation)
		} else {
			// Create serial channel
			channel, err := NewChannel(
				&portCfg,
				&m.config.Detection,
				&m.config.NATS,
				&m.config.Recovery,
				&m.config.App,
				&m.config.Logging,
				m.natsConn,
				m.logger.With("device", portCfg.Device),
			)
			if err != nil {
				m.logger.Error("Failed to create channel", "device", portCfg.Device, "error", err)
				continue
			}

			// Wire event callback - channel calls this, we publish to NATS
			// This keeps Channel decoupled from EventPublisher
			if m.eventPublisher != nil {
				channel.SetEventCallback(func(event output.Event) {
					m.eventPublisher.Publish(event)
				})
			}

			if err := channel.Start(ctx); err != nil {
				m.logger.Error("Failed to start channel", "device", portCfg.Device, "error", err)
				continue
			}

			m.mu.Lock()
			m.channels = append(m.channels, channel)
			m.mu.Unlock()

			startedCount++
			m.logger.Info("Started serial capture channel",
				"device", portCfg.Device,
				"a_designation", portCfg.ADesignation)
		}
	}

	if startedCount == 0 {
		return fmt.Errorf("failed to start any capture channels")
	}

	// Start health publisher
	healthSubject := output.BuildHealthSubject(m.config.NATS.SubjectPrefix, m.config.App.InstanceID)
	m.healthPublisher = output.NewHealthPublisher(&output.HealthPublisherConfig{
		Conn:       m.natsConn,
		Subject:    healthSubject,
		InstanceID: m.config.App.InstanceID,
		FIPSCode:   m.config.App.FIPSCode,
		Interval:   60 * time.Second,
		Logger:     m.logger,
		StatsFunc:  m.getHealthStats,
	})
	m.healthPublisher.Start()

	m.logger.Info("Capture manager started", "channels", startedCount)
	return nil
}

// Stop gracefully stops all capture channels
func (m *Manager) Stop() {
	m.logger.Info("Stopping capture manager")

	// Publish service stop event before shutting down
	if m.eventPublisher != nil {
		m.eventPublisher.PublishServiceStop("shutdown requested")
	}

	// Stop health publisher first (so it can send final heartbeat)
	if m.healthPublisher != nil {
		m.healthPublisher.Stop()
	}

	m.mu.RLock()
	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	m.mu.RUnlock()

	// Stop all channels concurrently
	var wg sync.WaitGroup
	for _, channel := range channels {
		wg.Add(1)
		go func(ch *Channel) {
			defer wg.Done()
			ch.Stop()
		}(channel)
	}

	wg.Wait()

	// Close NATS connection
	if m.natsConn != nil {
		m.natsConn.Close()
	}

	m.logger.Info("Capture manager stopped")
}

// GetChannels returns all capture channels
func (m *Manager) GetChannels() []*Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	return channels
}

// GetChannel returns a channel by device path
func (m *Manager) GetChannel(device string) *Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ch := range m.channels {
		if ch.Device() == device {
			return ch
		}
	}

	return nil
}

// GetStats returns statistics for all channels
func (m *Manager) GetStats() map[string]ChannelStats {
	m.mu.RLock()
	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	m.mu.RUnlock()

	stats := make(map[string]ChannelStats)
	for _, ch := range channels {
		stats[ch.Device()] = ch.Stats()
	}

	return stats
}

// GetStates returns states for all channels
func (m *Manager) GetStates() map[string]ChannelState {
	m.mu.RLock()
	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	m.mu.RUnlock()

	states := make(map[string]ChannelState)
	for _, ch := range channels {
		states[ch.Device()] = ch.State()
	}

	return states
}

// NATSConnected returns true if connected to NATS
func (m *Manager) NATSConnected() bool {
	return m.natsConn != nil && m.natsConn.IsConnected()
}

// NATSConn returns the NATS connection (for API event fetching)
func (m *Manager) NATSConn() *output.NATSConnection {
	return m.natsConn
}

// EventsSubject returns the NATS subject for events
func (m *Manager) EventsSubject() string {
	return output.BuildEventsSubject(m.config.NATS.SubjectPrefix, m.config.App.InstanceID)
}

// ChannelInfo contains channel information for API responses
type ChannelInfo struct {
	Device       string      `json:"device"`
	Path         string      `json:"path,omitempty"`
	Type         string      `json:"type"`
	ADesignation string      `json:"a_designation"`
	FIPSCode     string      `json:"fips_code"`
	State        string      `json:"state"`
	Stats        interface{} `json:"stats"`
}

// GetAllStats returns detailed stats for all channels (for API)
func (m *Manager) GetAllStats() map[string]interface{} {
	m.mu.RLock()
	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	httpChannels := make([]*HTTPChannel, len(m.httpChannels))
	copy(httpChannels, m.httpChannels)
	m.mu.RUnlock()

	channelInfos := make([]ChannelInfo, 0, len(channels)+len(httpChannels))

	// Serial channels
	for _, ch := range channels {
		// Get FIPS code (port-specific or app-level)
		fipsCode := ch.config.FIPSCode
		if fipsCode == "" {
			fipsCode = m.config.App.FIPSCode
		}

		channelInfos = append(channelInfos, ChannelInfo{
			Device:       ch.Device(),
			Type:         "serial",
			ADesignation: ch.config.ADesignation,
			FIPSCode:     fipsCode,
			State:        ch.State().String(),
			Stats:        ch.Stats(),
		})
	}

	// HTTP channels
	for _, ch := range httpChannels {
		cfg := ch.Config()
		fipsCode := cfg.FIPSCode
		if fipsCode == "" {
			fipsCode = m.config.App.FIPSCode
		}

		channelInfos = append(channelInfos, ChannelInfo{
			Path:         cfg.Path,
			Type:         "http",
			ADesignation: cfg.ADesignation,
			FIPSCode:     fipsCode,
			State:        "running",
			Stats:        ch.GetStats(),
		})
	}

	// Get NATS stats with JetStream stream info
	var natsStats *output.NATSStats
	if m.natsConn != nil {
		// Query JetStream for actual stream message counts
		streamNames := []string{"cdr", "health", "events"}
		stats := m.natsConn.StatsWithStreams(streamNames)
		natsStats = &stats
	}

	return map[string]interface{}{
		"instance_id":    m.config.App.InstanceID,
		"nats_connected": m.NATSConnected(),
		"nats":           natsStats,
		"channels":       channelInfos,
	}
}

// getHealthStats returns health stats for the health publisher
func (m *Manager) getHealthStats() output.HealthStats {
	m.mu.RLock()
	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	m.mu.RUnlock()

	now := time.Now()
	channelHealth := make([]output.ChannelHealth, 0, len(channels))

	for _, ch := range channels {
		stats := ch.Stats()

		// Calculate seconds since last line (-1 if never)
		var lastLineAgo int64 = -1
		if !stats.LastLineTime.IsZero() {
			lastLineAgo = int64(now.Sub(stats.LastLineTime).Seconds())
		}

		channelHealth = append(channelHealth, output.ChannelHealth{
			Device:       ch.Device(),
			ADesignation: ch.config.ADesignation,
			State:        ch.State().String(),
			BaudRate:     stats.DetectedBaud,
			Reconnects:   stats.Reconnects,
			BytesRead:    stats.BytesRead,
			LinesRead:    stats.LinesRead,
			Errors:       stats.Errors,
			LastLineAgo:  lastLineAgo,
		})
	}

	return output.HealthStats{
		NATSConnected: m.NATSConnected(),
		Channels:      channelHealth,
	}
}

// createHTTPChannel creates an HTTP capture channel with its DualWriter
func (m *Manager) createHTTPChannel(portCfg config.PortConfig) (*HTTPChannel, error) {
	// Get FIPS code
	fipsCode := portCfg.FIPSCode
	if fipsCode == "" {
		fipsCode = m.config.App.FIPSCode
	}

	// Build identifier for log file (e.g., "1429010002-A1")
	identifier := fmt.Sprintf("%s-%s", fipsCode, portCfg.ADesignation)

	// Build NATS subject
	var natsSubject string
	if portCfg.Vendor != "" {
		natsSubject = fmt.Sprintf("%s.%s.%s", m.config.NATS.SubjectPrefix, portCfg.Vendor, fipsCode)
	} else {
		natsSubject = fmt.Sprintf("%s.%s", m.config.NATS.SubjectPrefix, fipsCode)
	}

	// Create DualWriter config
	dwConfig := &output.DualWriterConfig{
		Device:        portCfg.Path, // Use path as device identifier for HTTP
		Identifier:    identifier,
		LogBasePath:   m.config.Logging.BasePath,
		LogMaxSizeMB:  m.config.Logging.MaxSizeMB,
		LogMaxBackups: m.config.Logging.MaxBackups,
		LogCompress:   m.config.Logging.Compress,
		NATSConn:      m.natsConn,
		NATSSubject:   natsSubject,
		Logger:        m.logger,
	}

	dualWriter, err := output.NewDualWriter(dwConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dual writer: %w", err)
	}

	return NewHTTPChannel(portCfg, m.config.App, dualWriter, m.logger), nil
}

// GetHTTPChannels returns all HTTP capture channels for route registration
func (m *Manager) GetHTTPChannels() []*HTTPChannel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channels := make([]*HTTPChannel, len(m.httpChannels))
	copy(channels, m.httpChannels)
	return channels
}
