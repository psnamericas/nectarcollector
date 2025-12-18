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

	manager := NewManager(cfg, "", logger)

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

	manager := NewManager(cfg, "", logger)
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

	manager := NewManager(cfg, "", logger)
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

	manager := NewManager(cfg, "", logger)
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

	manager := NewManager(cfg, "", logger)

	if manager.NATSConnected() {
		t.Error("NATSConnected() should return false when no connection")
	}
}

func TestManagerGetChannelNotFound(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
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

	manager := NewManager(cfg, "", logger)
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
		Device:          "/dev/ttyS1",
		Type:            "serial",
		SideDesignation: "A1",
		FIPSCode:        "1234567890",
		State:           "running",
		Stats:           stats,
	}

	if info.Device != "/dev/ttyS1" {
		t.Errorf("Device = %q, want %q", info.Device, "/dev/ttyS1")
	}
	if info.Type != "serial" {
		t.Errorf("Type = %q, want %q", info.Type, "serial")
	}
	if info.SideDesignation != "A1" {
		t.Errorf("SideDesignation = %q, want %q", info.SideDesignation, "A1")
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

func TestManagerGetPortConfigsEmpty(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	ports := manager.GetPortConfigs()

	if ports == nil {
		t.Error("GetPortConfigs() should return non-nil slice")
	}
	if len(ports) != 0 {
		t.Errorf("GetPortConfigs() should return empty slice, got %d", len(ports))
	}
}

func TestManagerGetPortConfigsWithPorts(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			FIPSCode: "3100000000",
		},
		Ports: []config.PortConfig{
			{
				Type:            "serial",
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
				BaudRate:        9600,
				Enabled:         true,
			},
			{
				Type:            "http",
				Path:            "/cdr",
				SideDesignation: "B1",
				Enabled:         true,
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	ports := manager.GetPortConfigs()

	if len(ports) != 2 {
		t.Fatalf("GetPortConfigs() returned %d ports, want 2", len(ports))
	}

	// Check serial port
	if ports[0].Type != "serial" {
		t.Errorf("Port 0 Type = %q, want %q", ports[0].Type, "serial")
	}
	if ports[0].Device != "/dev/ttyS1" {
		t.Errorf("Port 0 Device = %q, want %q", ports[0].Device, "/dev/ttyS1")
	}
	if ports[0].SideDesignation != "A1" {
		t.Errorf("Port 0 SideDesignation = %q, want %q", ports[0].SideDesignation, "A1")
	}
	if ports[0].FIPSCode != "3100000000" {
		t.Errorf("Port 0 FIPSCode = %q, want %q", ports[0].FIPSCode, "3100000000")
	}
	if ports[0].Config.BaudRate != 9600 {
		t.Errorf("Port 0 BaudRate = %d, want 9600", ports[0].Config.BaudRate)
	}

	// Check HTTP port
	if ports[1].Type != "http" {
		t.Errorf("Port 1 Type = %q, want %q", ports[1].Type, "http")
	}
	if ports[1].Path != "/cdr" {
		t.Errorf("Port 1 Path = %q, want %q", ports[1].Path, "/cdr")
	}
}

func TestManagerEnablePortNotFound(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.EnablePort("nonexistent")

	if err == nil {
		t.Error("EnablePort() should return error for non-existent port")
	}
}

func TestManagerDisablePortNotFound(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.DisablePort("nonexistent")

	if err == nil {
		t.Error("DisablePort() should return error for non-existent port")
	}
}

func TestManagerUpdatePortConfigNotFound(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.UpdatePortConfig("nonexistent", map[string]interface{}{
		"baud_rate": 9600,
	})

	if err == nil {
		t.Error("UpdatePortConfig() should return error for non-existent port")
	}
}

func TestManagerDeletePortNotFound(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.DeletePort("nonexistent")

	if err == nil {
		t.Error("DeletePort() should return error for non-existent port")
	}
}

func TestManagerAddPortDuplicate(t *testing.T) {
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.AddPort(config.PortConfig{
		Device:          "/dev/ttyS1",
		SideDesignation: "A2",
	})

	if err == nil {
		t.Error("AddPort() should return error for duplicate device")
	}
}

func TestManagerAddPortDuplicateSideDesignation(t *testing.T) {
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.AddPort(config.PortConfig{
		Device:          "/dev/ttyS2",
		SideDesignation: "A1",
	})

	if err == nil {
		t.Error("AddPort() should return error for duplicate side designation")
	}
}

func TestManagerEnableAlreadyEnabled(t *testing.T) {
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
				Enabled:         true,
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.EnablePort("ttyS1")

	if err == nil {
		t.Error("EnablePort() should return error for already enabled port")
	}
}

func TestManagerDisableAlreadyDisabled(t *testing.T) {
	cfg := &config.Config{
		Ports: []config.PortConfig{
			{
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
				Enabled:         false,
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	manager := NewManager(cfg, "", logger)
	err := manager.DisablePort("ttyS1")

	if err == nil {
		t.Error("DisablePort() should return error for already disabled port")
	}
}

func TestPortInfo(t *testing.T) {
	info := PortInfo{
		ID:              "ttyS1",
		Type:            "serial",
		Device:          "/dev/ttyS1",
		SideDesignation: "A1",
		FIPSCode:        "3100000000",
		Enabled:         true,
		State:           "running",
		Config: PortConfigDetails{
			BaudRate:       9600,
			DataBits:       8,
			Parity:         "none",
			StopBits:       1,
			UseFlowControl: nil,
		},
	}

	if info.ID != "ttyS1" {
		t.Errorf("ID = %q, want %q", info.ID, "ttyS1")
	}
	if info.Type != "serial" {
		t.Errorf("Type = %q, want %q", info.Type, "serial")
	}
	if info.Device != "/dev/ttyS1" {
		t.Errorf("Device = %q, want %q", info.Device, "/dev/ttyS1")
	}
	if info.Config.BaudRate != 9600 {
		t.Errorf("Config.BaudRate = %d, want 9600", info.Config.BaudRate)
	}
	if info.Config.DataBits != 8 {
		t.Errorf("Config.DataBits = %d, want 8", info.Config.DataBits)
	}
	if info.Config.Parity != "none" {
		t.Errorf("Config.Parity = %q, want %q", info.Config.Parity, "none")
	}
}

func TestPortInfoHTTP(t *testing.T) {
	info := PortInfo{
		ID:              "/cdr",
		Type:            "http",
		Path:            "/cdr",
		ListenPort:      8080,
		SideDesignation: "B1",
		FIPSCode:        "3100000000",
		Enabled:         true,
		State:           "running",
	}

	if info.ID != "/cdr" {
		t.Errorf("ID = %q, want %q", info.ID, "/cdr")
	}
	if info.Type != "http" {
		t.Errorf("Type = %q, want %q", info.Type, "http")
	}
	if info.Path != "/cdr" {
		t.Errorf("Path = %q, want %q", info.Path, "/cdr")
	}
	if info.ListenPort != 8080 {
		t.Errorf("ListenPort = %d, want 8080", info.ListenPort)
	}
}
