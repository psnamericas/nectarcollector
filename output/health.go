package output

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// HealthPublisher publishes periodic health heartbeats to NATS JetStream.
// These heartbeats enable fleet-wide monitoring and alerting.
type HealthPublisher struct {
	conn       *NATSConnection
	subject    string
	instanceID string
	fipsCode   string
	startTime  time.Time
	interval   time.Duration
	logger     *slog.Logger

	statsFunc func() HealthStats // Callback to get current stats

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// HealthStats contains the data needed for health messages.
// This is provided by the capture.Manager via callback.
type HealthStats struct {
	NATSConnected bool
	Channels      []ChannelHealth
}

// ChannelHealth contains per-channel health data
type ChannelHealth struct {
	Device       string `json:"device"`
	ADesignation string `json:"a"`
	State        string `json:"state"`
	BaudRate     int    `json:"baud"`       // Current detected baud rate
	Reconnects   int64  `json:"reconnects"` // Number of reconnection attempts
	BytesRead    int64  `json:"bytes"`
	LinesRead    int64  `json:"lines"`
	Errors       int64  `json:"errors"`
	LastLineAgo  int64  `json:"last_line_ago_sec"` // Seconds since last line, -1 if never
}

// HealthMessage is the JSON payload published to NATS
type HealthMessage struct {
	Version       int             `json:"v"`
	Timestamp     string          `json:"ts"`
	InstanceID    string          `json:"instance_id"`
	FIPSCode      string          `json:"fips_code"`
	UptimeSec     int64           `json:"uptime_sec"`
	NATSConnected bool            `json:"nats_connected"`
	Channels      []ChannelHealth `json:"channels"`
}

// HealthPublisherConfig contains configuration for HealthPublisher
type HealthPublisherConfig struct {
	Conn       *NATSConnection
	Subject    string        // e.g., "ne.health.psna-ne-kearney-01"
	InstanceID string        // e.g., "psna-ne-kearney-01"
	FIPSCode   string        // e.g., "1314010001"
	Interval   time.Duration // How often to publish (default 60s)
	Logger     *slog.Logger
	StatsFunc  func() HealthStats // Callback to get current stats
}

// NewHealthPublisher creates a new HealthPublisher
func NewHealthPublisher(cfg *HealthPublisherConfig) *HealthPublisher {
	interval := cfg.Interval
	if interval == 0 {
		interval = 60 * time.Second
	}

	return &HealthPublisher{
		conn:       cfg.Conn,
		subject:    cfg.Subject,
		instanceID: cfg.InstanceID,
		fipsCode:   cfg.FIPSCode,
		startTime:  time.Now(),
		interval:   interval,
		logger:     cfg.Logger,
		statsFunc:  cfg.StatsFunc,
		stopCh:     make(chan struct{}),
	}
}

// Start begins publishing health heartbeats
func (h *HealthPublisher) Start() {
	h.wg.Add(1)
	go h.publishLoop()
	h.logger.Info("Health publisher started",
		"subject", h.subject,
		"interval", h.interval)
}

// Stop stops the health publisher
func (h *HealthPublisher) Stop() {
	close(h.stopCh)
	h.wg.Wait()
	h.logger.Info("Health publisher stopped")
}

func (h *HealthPublisher) publishLoop() {
	defer h.wg.Done()

	// Publish immediately on start
	h.publish()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			// Publish final message before stopping
			h.publish()
			return
		case <-ticker.C:
			h.publish()
		}
	}
}

func (h *HealthPublisher) publish() {
	if h.conn == nil || !h.conn.IsConnected() {
		h.logger.Debug("Skipping health publish - NATS not connected")
		return
	}

	stats := h.statsFunc()

	msg := HealthMessage{
		Version:       1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		InstanceID:    h.instanceID,
		FIPSCode:      h.fipsCode,
		UptimeSec:     int64(time.Since(h.startTime).Seconds()),
		NATSConnected: stats.NATSConnected,
		Channels:      stats.Channels,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("Failed to marshal health message", "error", err)
		return
	}

	if err := h.conn.Publish(h.subject, data); err != nil {
		h.logger.Warn("Failed to publish health message", "error", err)
		return
	}

	h.logger.Debug("Published health heartbeat",
		"subject", h.subject,
		"uptime_sec", msg.UptimeSec,
		"channels", len(msg.Channels))
}

// BuildHealthSubject constructs the health subject from state prefix and hostname
// Format: {state}.health.{hostname}
func BuildHealthSubject(subjectPrefix, instanceID string) string {
	// subjectPrefix is like "ne.cdr", we want "ne.health.{instance}"
	// Extract state from prefix (first segment)
	state := subjectPrefix
	for i, c := range subjectPrefix {
		if c == '.' {
			state = subjectPrefix[:i]
			break
		}
	}
	return state + ".health." + instanceID
}
