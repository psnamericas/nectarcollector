package capture

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"nectarcollector/config"
	"nectarcollector/forward"
	"nectarcollector/output"
)

// Manager manages multiple capture channels (serial and HTTP)
type Manager struct {
	config          *config.Config
	configPath      string         // Path to config file for saving
	channels        []*Channel     // Serial channels
	httpChannels    []*HTTPChannel // HTTP channels
	natsConn        *output.NATSConnection
	healthPublisher *output.HealthPublisher
	eventPublisher  *output.EventPublisher
	forwarder       *forward.Forwarder
	logger          *slog.Logger
	ctx             context.Context // Context for starting new channels
	mu              sync.RWMutex
}

// NewManager creates a new capture manager
func NewManager(cfg *config.Config, configPath string, logger *slog.Logger) *Manager {
	return &Manager{
		config:       cfg,
		configPath:   configPath,
		channels:     make([]*Channel, 0),
		httpChannels: make([]*HTTPChannel, 0),
		logger:       logger,
	}
}

// Start initializes and starts all enabled capture channels.
// NATS connection is required - returns error if NATS is unavailable.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx // Store context for starting new channels later
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
				"side_designation", portCfg.SideDesignation)
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
				"side_designation", portCfg.SideDesignation)
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

	// Start forwarder if enabled
	if m.config.Forwarder.Enabled {
		m.forwarder = forward.New(&forward.ForwarderConfig{
			Config:     &m.config.Forwarder,
			InstanceID: m.config.App.InstanceID,
			LocalConn:  m.natsConn.Conn(),
			Logger:     m.logger.With("component", "forwarder"),
		})
		if err := m.forwarder.Start(ctx); err != nil {
			m.logger.Error("Failed to start forwarder", "error", err)
			// Non-fatal - capture continues without forwarding
		} else {
			m.logger.Info("Forwarder started", "remote_url", m.config.Forwarder.RemoteURL)
		}
	}

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

	// Stop forwarder first (drains pending messages)
	if m.forwarder != nil {
		m.forwarder.Stop()
	}

	// Stop health publisher (so it can send final heartbeat)
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
	Device          string      `json:"device"`
	Path            string      `json:"path,omitempty"`
	Type            string      `json:"type"`
	SideDesignation string      `json:"side_designation"`
	FIPSCode        string      `json:"fips_code"`
	State           string      `json:"state"`
	Stats           interface{} `json:"stats"`
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
			Device:          ch.Device(),
			Type:            "serial",
			SideDesignation: ch.config.SideDesignation,
			FIPSCode:        fipsCode,
			State:           ch.State().String(),
			Stats:           ch.Stats(),
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
			Path:            cfg.Path,
			Type:            "http",
			SideDesignation: cfg.SideDesignation,
			FIPSCode:        fipsCode,
			State:           "running",
			Stats:           ch.GetStats(),
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

	result := map[string]interface{}{
		"instance_id":    m.config.App.InstanceID,
		"nats_connected": m.NATSConnected(),
		"nats":           natsStats,
		"channels":       channelInfos,
	}

	// Add forwarder stats if enabled
	if m.forwarder != nil {
		result["forwarder"] = m.forwarder.Stats()
	}

	return result
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
			Device:          ch.Device(),
			SideDesignation: ch.config.SideDesignation,
			State:           ch.State().String(),
			BaudRate:        stats.DetectedBaud,
			Reconnects:      stats.Reconnects,
			BytesRead:       stats.BytesRead,
			LinesRead:       stats.LinesRead,
			Errors:          stats.Errors,
			LastLineAgo:     lastLineAgo,
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
	identifier := fmt.Sprintf("%s-%s", fipsCode, portCfg.SideDesignation)

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

// PortInfo contains port configuration and runtime state for API responses
type PortInfo struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	Device          string            `json:"device,omitempty"`
	Path            string            `json:"path,omitempty"`
	ListenPort      int               `json:"listen_port,omitempty"`
	SideDesignation string            `json:"side_designation"`
	FIPSCode        string            `json:"fips_code"`
	Vendor          string            `json:"vendor,omitempty"`
	Enabled         bool              `json:"enabled"`
	State           string            `json:"state"`
	Config          PortConfigDetails `json:"config"`
	Stats           interface{}       `json:"stats,omitempty"`
}

// PortConfigDetails contains configurable port settings
type PortConfigDetails struct {
	BaudRate       int     `json:"baud_rate,omitempty"`
	DataBits       int     `json:"data_bits,omitempty"`
	Parity         string  `json:"parity,omitempty"`
	StopBits       float64 `json:"stop_bits,omitempty"`
	UseFlowControl *bool   `json:"use_flow_control,omitempty"`
}

// GetPortConfigs returns all port configurations with their current state
func (m *Manager) GetPortConfigs() []PortInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ports := make([]PortInfo, 0, len(m.config.Ports))

	for i := range m.config.Ports {
		portCfg := &m.config.Ports[i]
		fipsCode := portCfg.FIPSCode
		if fipsCode == "" {
			fipsCode = m.config.App.FIPSCode
		}

		info := PortInfo{
			ID:              portCfg.ID(),
			SideDesignation: portCfg.SideDesignation,
			FIPSCode:        fipsCode,
			Vendor:          portCfg.Vendor,
			Enabled:         portCfg.Enabled,
		}

		if portCfg.IsHTTP() {
			info.Type = "http"
			info.Path = portCfg.Path
			info.ListenPort = portCfg.ListenPort

			// Find running HTTP channel
			for _, ch := range m.httpChannels {
				if ch.Path() == portCfg.Path {
					info.State = "running"
					info.Stats = ch.GetStats()
					break
				}
			}
			if info.State == "" {
				info.State = "stopped"
			}
		} else {
			info.Type = "serial"
			info.Device = portCfg.Device
			info.Config = PortConfigDetails{
				BaudRate:       portCfg.BaudRate,
				DataBits:       portCfg.DataBits,
				Parity:         portCfg.Parity,
				StopBits:       portCfg.StopBits,
				UseFlowControl: portCfg.UseFlowControl,
			}

			// Find running channel
			for _, ch := range m.channels {
				if ch.Device() == portCfg.Device {
					info.State = ch.State().String()
					info.Stats = ch.Stats()
					break
				}
			}
			if info.State == "" {
				info.State = "stopped"
			}
		}

		ports = append(ports, info)
	}

	return ports
}

// findPortIndex finds a port config by ID and returns its index
func (m *Manager) findPortIndex(id string) int {
	for i := range m.config.Ports {
		if m.config.Ports[i].ID() == id {
			return i
		}
	}
	return -1
}

// EnablePort enables a disabled port and starts its channel
func (m *Manager) EnablePort(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findPortIndex(id)
	if idx < 0 {
		return fmt.Errorf("port not found: %s", id)
	}

	portCfg := &m.config.Ports[idx]
	if portCfg.Enabled {
		return fmt.Errorf("port already enabled: %s", id)
	}

	portCfg.Enabled = true

	// Start the channel
	if err := m.startChannelLocked(portCfg); err != nil {
		portCfg.Enabled = false // Rollback on failure
		return fmt.Errorf("failed to start channel: %w", err)
	}

	// Save config
	if err := m.config.Save(m.configPath); err != nil {
		m.logger.Warn("Failed to save config after enabling port", "id", id, "error", err)
	}

	m.logger.Info("Enabled port", "id", id)
	return nil
}

// DisablePort disables a running port and stops its channel
func (m *Manager) DisablePort(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findPortIndex(id)
	if idx < 0 {
		return fmt.Errorf("port not found: %s", id)
	}

	portCfg := &m.config.Ports[idx]
	if !portCfg.Enabled {
		return fmt.Errorf("port already disabled: %s", id)
	}

	// Stop the channel
	if err := m.stopChannelLocked(portCfg); err != nil {
		return fmt.Errorf("failed to stop channel: %w", err)
	}

	portCfg.Enabled = false

	// Save config
	if err := m.config.Save(m.configPath); err != nil {
		m.logger.Warn("Failed to save config after disabling port", "id", id, "error", err)
	}

	m.logger.Info("Disabled port", "id", id)
	return nil
}

// UpdatePortConfig updates port settings and restarts the channel if needed
func (m *Manager) UpdatePortConfig(id string, updates map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findPortIndex(id)
	if idx < 0 {
		return fmt.Errorf("port not found: %s", id)
	}

	portCfg := &m.config.Ports[idx]
	wasEnabled := portCfg.Enabled
	needsRestart := false

	// Apply updates
	for key, value := range updates {
		switch key {
		case "baud_rate":
			if v, ok := value.(float64); ok {
				portCfg.BaudRate = int(v)
				needsRestart = true
			}
		case "data_bits":
			if v, ok := value.(float64); ok {
				portCfg.DataBits = int(v)
				needsRestart = true
			}
		case "parity":
			if v, ok := value.(string); ok {
				portCfg.Parity = v
				needsRestart = true
			}
		case "stop_bits":
			if v, ok := value.(float64); ok {
				portCfg.StopBits = v
				needsRestart = true
			}
		case "use_flow_control":
			if v, ok := value.(bool); ok {
				portCfg.UseFlowControl = &v
				needsRestart = true
			} else if value == nil {
				portCfg.UseFlowControl = nil
				needsRestart = true
			}
		case "listen_port":
			if v, ok := value.(float64); ok {
				portCfg.ListenPort = int(v)
				needsRestart = true
			}
		case "path":
			if v, ok := value.(string); ok && portCfg.IsHTTP() {
				portCfg.Path = v
				needsRestart = true
			}
		case "side_designation":
			if v, ok := value.(string); ok {
				portCfg.SideDesignation = v
				needsRestart = true
			}
		case "fips_code":
			if v, ok := value.(string); ok {
				portCfg.FIPSCode = v
				needsRestart = true
			}
		case "vendor":
			if v, ok := value.(string); ok {
				portCfg.Vendor = v
				needsRestart = true
			}
		case "county":
			if v, ok := value.(string); ok {
				portCfg.County = v
			}
		case "description":
			if v, ok := value.(string); ok {
				portCfg.Description = v
			}
		default:
			return fmt.Errorf("unknown config field: %s", key)
		}
	}

	// Restart channel if needed and was running
	if needsRestart && wasEnabled {
		if err := m.stopChannelLocked(portCfg); err != nil {
			m.logger.Warn("Failed to stop channel for update", "id", id, "error", err)
		}
		if err := m.startChannelLocked(portCfg); err != nil {
			return fmt.Errorf("failed to restart channel: %w", err)
		}
	}

	// Save config
	if err := m.config.Save(m.configPath); err != nil {
		m.logger.Warn("Failed to save config after update", "id", id, "error", err)
	}

	m.logger.Info("Updated port config", "id", id, "updates", updates)
	return nil
}

// AddPort adds a new port configuration
func (m *Manager) AddPort(portCfg config.PortConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate required fields
	if portCfg.SideDesignation == "" {
		return fmt.Errorf("side_designation is required")
	}

	if portCfg.IsHTTP() {
		if portCfg.Path == "" {
			return fmt.Errorf("path is required for HTTP ports")
		}
		// Check for duplicate path
		for _, p := range m.config.Ports {
			if p.IsHTTP() && p.Path == portCfg.Path {
				return fmt.Errorf("HTTP path already exists: %s", portCfg.Path)
			}
		}
	} else {
		if portCfg.Device == "" {
			return fmt.Errorf("device is required for serial ports")
		}
		// Check for duplicate device
		for _, p := range m.config.Ports {
			if p.IsSerial() && p.Device == portCfg.Device {
				return fmt.Errorf("device already configured: %s", portCfg.Device)
			}
		}
	}

	// Check for duplicate side designation
	for _, p := range m.config.Ports {
		if p.SideDesignation == portCfg.SideDesignation {
			return fmt.Errorf("side_designation already in use: %s", portCfg.SideDesignation)
		}
	}

	// Set defaults
	if portCfg.IsSerial() {
		if portCfg.DataBits == 0 {
			portCfg.DataBits = 8
		}
		if portCfg.StopBits == 0 {
			portCfg.StopBits = 1
		}
		if portCfg.Parity == "" {
			portCfg.Parity = "none"
		}
	}

	// Add to config
	m.config.Ports = append(m.config.Ports, portCfg)

	// Start if enabled
	if portCfg.Enabled {
		if err := m.startChannelLocked(&m.config.Ports[len(m.config.Ports)-1]); err != nil {
			// Remove from config on failure
			m.config.Ports = m.config.Ports[:len(m.config.Ports)-1]
			return fmt.Errorf("failed to start channel: %w", err)
		}
	}

	// Save config
	if err := m.config.Save(m.configPath); err != nil {
		m.logger.Warn("Failed to save config after adding port", "error", err)
	}

	m.logger.Info("Added port", "id", portCfg.ID(), "type", portCfg.Type)
	return nil
}

// DeletePort removes a port configuration
func (m *Manager) DeletePort(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findPortIndex(id)
	if idx < 0 {
		return fmt.Errorf("port not found: %s", id)
	}

	portCfg := &m.config.Ports[idx]

	// Stop channel if running
	if portCfg.Enabled {
		if err := m.stopChannelLocked(portCfg); err != nil {
			m.logger.Warn("Failed to stop channel before delete", "id", id, "error", err)
		}
	}

	// Remove from config
	m.config.Ports = append(m.config.Ports[:idx], m.config.Ports[idx+1:]...)

	// Save config
	if err := m.config.Save(m.configPath); err != nil {
		m.logger.Warn("Failed to save config after deleting port", "id", id, "error", err)
	}

	m.logger.Info("Deleted port", "id", id)
	return nil
}

// GetAvailableSerialPorts returns a list of serial ports not currently configured
func (m *Manager) GetAvailableSerialPorts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Standard COM ports on Linux (ttyS1-ttyS5, skipping ttyS0 which is console)
	allPorts := []string{
		"/dev/ttyS1", "/dev/ttyS2", "/dev/ttyS3", "/dev/ttyS4", "/dev/ttyS5",
	}

	// Build set of configured devices
	configured := make(map[string]bool)
	for _, p := range m.config.Ports {
		if p.IsSerial() {
			configured[p.Device] = true
		}
	}

	// Return unconfigured ports
	available := make([]string, 0)
	for _, port := range allPorts {
		if !configured[port] {
			available = append(available, port)
		}
	}

	return available
}

// startChannelLocked starts a channel for the given port config (must hold lock)
func (m *Manager) startChannelLocked(portCfg *config.PortConfig) error {
	if portCfg.IsHTTP() {
		httpChannel, err := m.createHTTPChannel(*portCfg)
		if err != nil {
			return err
		}
		m.httpChannels = append(m.httpChannels, httpChannel)
		m.logger.Info("Started HTTP channel", "path", portCfg.Path)
	} else {
		channel, err := NewChannel(
			portCfg,
			&m.config.Detection,
			&m.config.NATS,
			&m.config.Recovery,
			&m.config.App,
			&m.config.Logging,
			m.natsConn,
			m.logger.With("device", portCfg.Device),
		)
		if err != nil {
			return err
		}

		if m.eventPublisher != nil {
			channel.SetEventCallback(func(event output.Event) {
				m.eventPublisher.Publish(event)
			})
		}

		if err := channel.Start(m.ctx); err != nil {
			return err
		}

		m.channels = append(m.channels, channel)
		m.logger.Info("Started serial channel", "device", portCfg.Device)
	}
	return nil
}

// stopChannelLocked stops a channel for the given port config (must hold lock)
func (m *Manager) stopChannelLocked(portCfg *config.PortConfig) error {
	if portCfg.IsHTTP() {
		for i, ch := range m.httpChannels {
			if ch.Path() == portCfg.Path {
				if err := ch.Stop(); err != nil {
					return err
				}
				m.httpChannels = append(m.httpChannels[:i], m.httpChannels[i+1:]...)
				m.logger.Info("Stopped HTTP channel", "path", portCfg.Path)
				return nil
			}
		}
	} else {
		for i, ch := range m.channels {
			if ch.Device() == portCfg.Device {
				ch.Stop()
				m.channels = append(m.channels[:i], m.channels[i+1:]...)
				m.logger.Info("Stopped serial channel", "device", portCfg.Device)
				return nil
			}
		}
	}
	return nil
}

// Config returns the current configuration
func (m *Manager) Config() *config.Config {
	return m.config
}
