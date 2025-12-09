package capture

import (
	"fmt"
	"testing"
	"time"

	"nectarcollector/serial"
)

func TestChannelStateString(t *testing.T) {
	tests := []struct {
		state ChannelState
		want  string
	}{
		{StateDetecting, "detecting"},
		{StateRunning, "running"},
		{StateNoSignal, "no_signal"},
		{StateReconnecting, "reconnecting"},
		{StateWaitingForNATS, "waiting_for_nats"},
		{StateStopped, "stopped"},
		{StateError, "error"},
		{ChannelState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("ChannelState.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannelStatsDefaults(t *testing.T) {
	stats := ChannelStats{}

	if stats.BytesRead != 0 {
		t.Errorf("BytesRead = %d, want 0", stats.BytesRead)
	}
	if stats.LinesRead != 0 {
		t.Errorf("LinesRead = %d, want 0", stats.LinesRead)
	}
	if stats.Errors != 0 {
		t.Errorf("Errors = %d, want 0", stats.Errors)
	}
	if stats.Reconnects != 0 {
		t.Errorf("Reconnects = %d, want 0", stats.Reconnects)
	}
	if stats.DetectedBaud != 0 {
		t.Errorf("DetectedBaud = %d, want 0", stats.DetectedBaud)
	}
	if !stats.LastLineTime.IsZero() {
		t.Error("LastLineTime should be zero")
	}
	if !stats.StartTime.IsZero() {
		t.Error("StartTime should be zero")
	}
}

func TestChannelStatsTimestamps(t *testing.T) {
	now := time.Now()
	stats := ChannelStats{
		StartTime:    now,
		LastLineTime: now.Add(5 * time.Second),
	}

	if stats.StartTime != now {
		t.Error("StartTime not set correctly")
	}
	if stats.LastLineTime.Sub(stats.StartTime) != 5*time.Second {
		t.Error("LastLineTime not set correctly")
	}
}

func TestBufferSizeConstants(t *testing.T) {
	// Verify buffer sizes are reasonable
	if InitialLineBufferSize < 1024 {
		t.Errorf("InitialLineBufferSize = %d, should be at least 1KB", InitialLineBufferSize)
	}
	if InitialLineBufferSize > 1024*1024 {
		t.Errorf("InitialLineBufferSize = %d, should be at most 1MB", InitialLineBufferSize)
	}

	if MaxLineBufferSize < InitialLineBufferSize {
		t.Errorf("MaxLineBufferSize (%d) should be >= InitialLineBufferSize (%d)",
			MaxLineBufferSize, InitialLineBufferSize)
	}
	if MaxLineBufferSize > 10*1024*1024 {
		t.Errorf("MaxLineBufferSize = %d, should be at most 10MB", MaxLineBufferSize)
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		errMsg    string
		isTimeout bool
	}{
		{"bufio.Scanner: token too long", true},
		{"unexpected EOF", true},
		{"i/o timeout", true},
		{"resource temporarily unavailable", true},
		{"serial read timeout", true},
		{"connection refused", false},
		{"permission denied", false},
		{"no such device", false},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			err := fmt.Errorf("%s", tt.errMsg)
			got := isTimeoutError(err)
			if got != tt.isTimeout {
				t.Errorf("isTimeoutError(%q) = %v, want %v", tt.errMsg, got, tt.isTimeout)
			}
		})
	}

	// Test with serial.ErrReadTimeout directly
	t.Run("serial.ErrReadTimeout", func(t *testing.T) {
		if !isTimeoutError(serial.ErrReadTimeout) {
			t.Error("isTimeoutError(serial.ErrReadTimeout) should be true")
		}
	})
}

func TestNATSCheckInterval(t *testing.T) {
	// Verify the check interval is reasonable
	if natsCheckInterval < 100*time.Millisecond {
		t.Errorf("natsCheckInterval = %v, should be at least 100ms", natsCheckInterval)
	}
	if natsCheckInterval > 5*time.Second {
		t.Errorf("natsCheckInterval = %v, should be at most 5s", natsCheckInterval)
	}
}

// MockNATSChecker implements NATSChecker for testing
type MockNATSChecker struct {
	connected bool
}

func (m *MockNATSChecker) IsConnected() bool {
	return m.connected
}

func TestMockNATSChecker(t *testing.T) {
	checker := &MockNATSChecker{connected: true}
	if !checker.IsConnected() {
		t.Error("MockNATSChecker.IsConnected() should return true")
	}

	checker.connected = false
	if checker.IsConnected() {
		t.Error("MockNATSChecker.IsConnected() should return false")
	}
}
