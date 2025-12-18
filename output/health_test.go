package output

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHealthMessageJSON(t *testing.T) {
	msg := HealthMessage{
		Version:       1,
		Timestamp:     "2025-12-05T18:30:00Z",
		InstanceID:    "psna-ne-kearney-01",
		FIPSCode:      "1314010001",
		UptimeSec:     86400,
		NATSConnected: true,
		Channels: []ChannelHealth{
			{
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
				State:           "running",
				BaudRate:        9600,
				Reconnects:      0,
				BytesRead:       1234567,
				LinesRead:       5432,
				Errors:          0,
				LastLineAgo:     5,
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify it can be unmarshaled
	var parsed HealthMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsed.Version != 1 {
		t.Errorf("Version = %d, want 1", parsed.Version)
	}
	if parsed.InstanceID != "psna-ne-kearney-01" {
		t.Errorf("InstanceID = %q, want %q", parsed.InstanceID, "psna-ne-kearney-01")
	}
	if parsed.UptimeSec != 86400 {
		t.Errorf("UptimeSec = %d, want 86400", parsed.UptimeSec)
	}
	if len(parsed.Channels) != 1 {
		t.Errorf("len(Channels) = %d, want 1", len(parsed.Channels))
	}
}

func TestHealthMessageSize(t *testing.T) {
	// Verify message size is reasonable for 30-day retention
	msg := HealthMessage{
		Version:       1,
		Timestamp:     "2025-12-05T18:30:00Z",
		InstanceID:    "psna-ne-southcentralpan-kearney-01",
		FIPSCode:      "1314010001",
		UptimeSec:     2592000, // 30 days
		NATSConnected: true,
		Channels: []ChannelHealth{
			{
				Device:          "/dev/ttyS1",
				SideDesignation: "A1",
				State:           "running",
				BaudRate:        115200,
				Reconnects:      99,
				BytesRead:       999999999,
				LinesRead:       9999999,
				Errors:          999,
				LastLineAgo:     999999,
			},
			{
				Device:          "/dev/ttyS2",
				SideDesignation: "A2",
				State:           "running",
				BaudRate:        115200,
				Reconnects:      99,
				BytesRead:       999999999,
				LinesRead:       9999999,
				Errors:          999,
				LastLineAgo:     999999,
			},
		},
	}

	data, _ := json.Marshal(msg)

	// Should be under 1KB per message
	if len(data) > 1024 {
		t.Errorf("Message size = %d bytes, should be under 1KB", len(data))
	}

	// Calculate 30-day storage at 60s intervals
	// 30 days * 24 hours * 60 messages/hour = 43,200 messages
	// 43,200 * len(data) = total bytes
	messagesPerMonth := 30 * 24 * 60
	totalBytes := messagesPerMonth * len(data)
	totalMB := float64(totalBytes) / (1024 * 1024)

	t.Logf("Message size: %d bytes", len(data))
	t.Logf("30-day storage estimate: %.2f MB", totalMB)

	// Should be under 50MB for 30 days
	if totalMB > 50 {
		t.Errorf("30-day storage = %.2f MB, should be under 50MB", totalMB)
	}
}

func TestBuildHealthSubject(t *testing.T) {
	tests := []struct {
		prefix     string
		instanceID string
		want       string
	}{
		{"ne.cdr", "psna-ne-kearney-01", "ne.health.psna-ne-kearney-01"},
		{"tx.cdr", "psna-tx-dallas-01", "tx.health.psna-tx-dallas-01"},
		{"ca.cdr.vendor", "psna-ca-la-01", "ca.health.psna-ca-la-01"},
		{"simple", "instance", "simple.health.instance"}, // No dot in prefix
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := BuildHealthSubject(tt.prefix, tt.instanceID)
			if got != tt.want {
				t.Errorf("BuildHealthSubject(%q, %q) = %q, want %q",
					tt.prefix, tt.instanceID, got, tt.want)
			}
		})
	}
}

func TestChannelHealthJSON(t *testing.T) {
	ch := ChannelHealth{
		Device:          "/dev/ttyS1",
		SideDesignation: "A1",
		State:           "waiting_for_nats",
		BaudRate:        9600,
		Reconnects:      0,
		BytesRead:       0,
		LinesRead:       0,
		Errors:          0,
		LastLineAgo:     -1,
	}

	data, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify JSON field names are short
	jsonStr := string(data)
	if !contains(jsonStr, `"a"`) {
		t.Error("SideDesignation should serialize as 'a'")
	}
	if !contains(jsonStr, `"baud"`) {
		t.Error("BaudRate should serialize as 'baud'")
	}
	if !contains(jsonStr, `"last_line_ago_sec"`) {
		t.Error("LastLineAgo should serialize as 'last_line_ago_sec'")
	}
}

func TestHealthPublisherConfig(t *testing.T) {
	cfg := &HealthPublisherConfig{
		Conn:       nil,
		Subject:    "ne.health.test",
		InstanceID: "test-01",
		FIPSCode:   "1234567890",
		Interval:   30 * time.Second,
		Logger:     nil,
		StatsFunc:  func() HealthStats { return HealthStats{} },
	}

	if cfg.Subject != "ne.health.test" {
		t.Errorf("Subject = %q, want %q", cfg.Subject, "ne.health.test")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", cfg.Interval)
	}
}

func TestHealthStatsDefaults(t *testing.T) {
	stats := HealthStats{}

	if stats.NATSConnected {
		t.Error("NATSConnected should default to false")
	}
	if stats.Channels != nil {
		t.Error("Channels should default to nil")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
