package capture

import (
	"log/slog"
	"os"
	"testing"

	"nectarcollector/config"
)

func TestNewManager(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Name:       "Test",
			InstanceID: "test-01",
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)

	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}
	if manager.config != cfg {
		t.Error("Manager config not set correctly")
	}
	if manager.logger != logger {
		t.Error("Manager logger not set correctly")
	}
	if len(manager.channels) != 0 {
		t.Errorf("Manager channels should be empty, got %d", len(manager.channels))
	}
}

func TestManagerGetChannelsEmpty(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)
	channels := manager.GetChannels()

	if channels == nil {
		t.Error("GetChannels() should return non-nil slice")
	}
	if len(channels) != 0 {
		t.Errorf("GetChannels() should return empty slice, got %d", len(channels))
	}
}

func TestManagerGetStatsEmpty(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)
	stats := manager.GetStats()

	if stats == nil {
		t.Error("GetStats() should return non-nil map")
	}
	if len(stats) != 0 {
		t.Errorf("GetStats() should return empty map, got %d", len(stats))
	}
}

func TestManagerGetStatesEmpty(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)
	states := manager.GetStates()

	if states == nil {
		t.Error("GetStates() should return non-nil map")
	}
	if len(states) != 0 {
		t.Errorf("GetStates() should return empty map, got %d", len(states))
	}
}

func TestManagerNATSConnectedNoConnection(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)

	if manager.NATSConnected() {
		t.Error("NATSConnected() should return false when no connection")
	}
}

func TestManagerGetChannelNotFound(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)
	channel := manager.GetChannel("/dev/ttyS1")

	if channel != nil {
		t.Error("GetChannel() should return nil for non-existent device")
	}
}

func TestManagerGetAllStatsEmpty(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Name:       "Test",
			InstanceID: "test-01",
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, logger)
	allStats := manager.GetAllStats()

	if allStats == nil {
		t.Error("GetAllStats() should return non-nil map")
	}

	instanceID, ok := allStats["instance_id"].(string)
	if !ok || instanceID != "test-01" {
		t.Errorf("GetAllStats() instance_id = %v, want %q", allStats["instance_id"], "test-01")
	}

	natsConnected, ok := allStats["nats_connected"].(bool)
	if !ok || natsConnected {
		t.Errorf("GetAllStats() nats_connected = %v, want false", allStats["nats_connected"])
	}

	channels, ok := allStats["channels"].([]ChannelInfo)
	if !ok || len(channels) != 0 {
		t.Errorf("GetAllStats() channels should be empty slice")
	}
}

func TestChannelInfo(t *testing.T) {
	stats := ChannelStats{
		BytesRead: 1000,
		LinesRead: 50,
	}
	info := ChannelInfo{
		Device:       "/dev/ttyS1",
		Type:         "serial",
		ADesignation: "A1",
		FIPSCode:     "1234567890",
		State:        "running",
		Stats:        stats,
	}

	if info.Device != "/dev/ttyS1" {
		t.Errorf("Device = %q, want %q", info.Device, "/dev/ttyS1")
	}
	if info.Type != "serial" {
		t.Errorf("Type = %q, want %q", info.Type, "serial")
	}
	if info.ADesignation != "A1" {
		t.Errorf("ADesignation = %q, want %q", info.ADesignation, "A1")
	}
	if info.FIPSCode != "1234567890" {
		t.Errorf("FIPSCode = %q, want %q", info.FIPSCode, "1234567890")
	}
	if info.State != "running" {
		t.Errorf("State = %q, want %q", info.State, "running")
	}
	// Stats is now interface{}, type assert to verify
	if s, ok := info.Stats.(ChannelStats); ok {
		if s.BytesRead != 1000 {
			t.Errorf("Stats.BytesRead = %d, want 1000", s.BytesRead)
		}
	} else {
		t.Errorf("Stats should be ChannelStats type")
	}
}
