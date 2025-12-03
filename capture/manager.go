package capture

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"nectarcollector/config"
	"nectarcollector/output"
)

// Manager manages multiple capture channels
type Manager struct {
	config   *config.Config
	channels []*Channel
	natsConn *output.NATSConnection
	logger   *slog.Logger
	mu       sync.RWMutex
}

// NewManager creates a new capture manager
func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		config:   cfg,
		channels: make([]*Channel, 0),
		logger:   logger,
	}
}

// Start initializes and starts all enabled capture channels
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting capture manager", "instance", m.config.App.InstanceID)

	// Connect to NATS
	natsConn, err := output.NewNATSConnection(
		m.config.NATS.URL,
		m.config.NATS.MaxReconnects,
		m.logger,
	)
	if err != nil {
		m.logger.Warn("Failed to connect to NATS, continuing without NATS", "error", err)
		// Continue without NATS - log files will still work
	} else {
		m.natsConn = natsConn
	}

	// Create and start channels for enabled ports
	startedCount := 0
	for _, portCfg := range m.config.Ports {
		if !portCfg.Enabled {
			m.logger.Info("Skipping disabled port", "device", portCfg.Device)
			continue
		}

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

		if err := channel.Start(ctx); err != nil {
			m.logger.Error("Failed to start channel", "device", portCfg.Device, "error", err)
			continue
		}

		m.mu.Lock()
		m.channels = append(m.channels, channel)
		m.mu.Unlock()

		startedCount++
		m.logger.Info("Started capture channel",
			"device", portCfg.Device,
			"a_designation", portCfg.ADesignation)
	}

	if startedCount == 0 {
		return fmt.Errorf("failed to start any capture channels")
	}

	m.logger.Info("Capture manager started", "channels", startedCount)
	return nil
}

// Stop gracefully stops all capture channels
func (m *Manager) Stop() {
	m.logger.Info("Stopping capture manager")

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

// ChannelInfo contains channel information for API responses
type ChannelInfo struct {
	Device       string        `json:"device"`
	ADesignation string        `json:"a_designation"`
	FIPSCode     string        `json:"fips_code"`
	State        string        `json:"state"`
	Stats        ChannelStats  `json:"stats"`
}

// GetAllStats returns detailed stats for all channels (for API)
func (m *Manager) GetAllStats() map[string]interface{} {
	m.mu.RLock()
	channels := make([]*Channel, len(m.channels))
	copy(channels, m.channels)
	m.mu.RUnlock()

	channelInfos := make([]ChannelInfo, 0, len(channels))
	for _, ch := range channels {
		// Get FIPS code (port-specific or app-level)
		fipsCode := ch.config.FIPSCode
		if fipsCode == "" {
			fipsCode = m.config.App.FIPSCode
		}

		channelInfos = append(channelInfos, ChannelInfo{
			Device:       ch.Device(),
			ADesignation: ch.config.ADesignation,
			FIPSCode:     fipsCode,
			State:        ch.State().String(),
			Stats:        ch.Stats(),
		})
	}

	return map[string]interface{}{
		"instance_id":    m.config.App.InstanceID,
		"nats_connected": m.NATSConnected(),
		"channels":       channelInfos,
	}
}
