package serial

import (
	"io"
	"sync"
	"testing"
	"time"
)

// MockReader implements Reader for testing
type MockReader struct {
	device         string
	isOpen         bool
	data           []byte
	readIndex      int
	readDelay      time.Duration
	readErr        error
	closeErr       error
	reconfigureCnt int
	mu             sync.Mutex
}

func NewMockReader(device string, data []byte) *MockReader {
	return &MockReader{
		device: device,
		isOpen: true,
		data:   data,
	}
}

func (m *MockReader) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.readDelay > 0 {
		time.Sleep(m.readDelay)
	}

	if m.readErr != nil {
		return 0, m.readErr
	}

	if !m.isOpen {
		return 0, io.EOF
	}

	if m.readIndex >= len(m.data) {
		return 0, io.EOF
	}

	n = copy(p, m.data[m.readIndex:])
	m.readIndex += n
	return n, nil
}

func (m *MockReader) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isOpen = false
	return m.closeErr
}

func (m *MockReader) Device() string {
	return m.device
}

func (m *MockReader) IsOpen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isOpen
}

func (m *MockReader) Reconfigure(baudRate int, useFlowControl bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reconfigureCnt++
	return nil
}

func (m *MockReader) SetReadTimeout(timeout time.Duration) error {
	return nil
}

func (m *MockReader) ResetInputBuffer() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readIndex = 0
	return nil
}

func (m *MockReader) GetModemStatus() (*ModemStatus, error) {
	return &ModemStatus{
		CTS: true,
		DSR: true,
		DCD: true,
		RI:  false,
	}, nil
}

func TestReaderWithStats(t *testing.T) {
	mockData := []byte("Hello World\nSecond Line\n")
	mock := NewMockReader("/dev/ttyS1", mockData)

	reader := NewReaderWithStats(mock)

	// Read all data
	buf := make([]byte, 100)
	totalRead := 0
	for {
		n, err := reader.Read(buf[totalRead:])
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	// Check bytes read stat
	bytesRead, linesRead, errors := reader.Stats()
	if bytesRead != int64(len(mockData)) {
		t.Errorf("BytesRead = %d, want %d", bytesRead, len(mockData))
	}

	// Lines should be 0 until LineRead() is called
	if linesRead != 0 {
		t.Errorf("LinesRead = %d, want 0", linesRead)
	}

	if errors != 0 {
		t.Errorf("Errors = %d, want 0", errors)
	}
}

func TestReaderWithStatsLineRead(t *testing.T) {
	mock := NewMockReader("/dev/ttyS1", []byte{})
	reader := NewReaderWithStats(mock)

	// Simulate reading lines
	reader.LineRead()
	reader.LineRead()
	reader.LineRead()

	_, linesRead, _ := reader.Stats()
	if linesRead != 3 {
		t.Errorf("LinesRead = %d, want 3", linesRead)
	}
}

func TestReaderWithStatsIncrementErrors(t *testing.T) {
	mock := NewMockReader("/dev/ttyS1", []byte{})
	reader := NewReaderWithStats(mock)

	reader.IncrementErrors()
	reader.IncrementErrors()

	_, _, errors := reader.Stats()
	if errors != 2 {
		t.Errorf("Errors = %d, want 2", errors)
	}
}

func TestReaderWithStatsReset(t *testing.T) {
	mock := NewMockReader("/dev/ttyS1", []byte("test"))
	reader := NewReaderWithStats(mock)

	// Accumulate some stats
	buf := make([]byte, 10)
	reader.Read(buf)
	reader.LineRead()
	reader.IncrementErrors()

	// Verify stats are non-zero
	b, l, e := reader.Stats()
	if b == 0 || l == 0 || e == 0 {
		t.Error("Stats should be non-zero before reset")
	}

	// Reset
	reader.ResetStats()

	// Verify stats are zero
	b, l, e = reader.Stats()
	if b != 0 || l != 0 || e != 0 {
		t.Errorf("Stats after reset: bytes=%d, lines=%d, errors=%d, want all zero", b, l, e)
	}
}

func TestReaderWithStatsDevice(t *testing.T) {
	mock := NewMockReader("/dev/ttyUSB0", []byte{})
	reader := NewReaderWithStats(mock)

	if reader.Device() != "/dev/ttyUSB0" {
		t.Errorf("Device() = %q, want %q", reader.Device(), "/dev/ttyUSB0")
	}
}

func TestReaderWithStatsIsOpen(t *testing.T) {
	mock := NewMockReader("/dev/ttyS1", []byte{})
	reader := NewReaderWithStats(mock)

	if !reader.IsOpen() {
		t.Error("IsOpen() should return true initially")
	}

	reader.Close()

	if reader.IsOpen() {
		t.Error("IsOpen() should return false after Close()")
	}
}

func TestSerialConfigDefaults(t *testing.T) {
	cfg := DefaultSerialConfig(9600, false)

	if cfg.BaudRate != 9600 {
		t.Errorf("BaudRate = %d, want 9600", cfg.BaudRate)
	}
	if cfg.DataBits != 8 {
		t.Errorf("DataBits = %d, want 8", cfg.DataBits)
	}
	if cfg.Parity != "none" {
		t.Errorf("Parity = %q, want %q", cfg.Parity, "none")
	}
	if cfg.StopBits != 1 {
		t.Errorf("StopBits = %d, want 1", cfg.StopBits)
	}
	if cfg.UseFlowControl {
		t.Error("UseFlowControl should be false")
	}
}

func TestSerialConfigWithFlowControl(t *testing.T) {
	cfg := DefaultSerialConfig(115200, true)

	if cfg.BaudRate != 115200 {
		t.Errorf("BaudRate = %d, want 115200", cfg.BaudRate)
	}
	if !cfg.UseFlowControl {
		t.Error("UseFlowControl should be true")
	}
}

func TestModemStatus(t *testing.T) {
	status := ModemStatus{
		CTS: true,
		DSR: true,
		DCD: false,
		RI:  false,
	}

	if !status.CTS {
		t.Error("CTS should be true")
	}
	if !status.DSR {
		t.Error("DSR should be true")
	}
	if status.DCD {
		t.Error("DCD should be false")
	}
	if status.RI {
		t.Error("RI should be false")
	}
}

func TestDefaultReadTimeout(t *testing.T) {
	if DefaultReadTimeout < 100*time.Millisecond || DefaultReadTimeout > 5*time.Second {
		t.Errorf("DefaultReadTimeout = %v, expected between 100ms and 5s", DefaultReadTimeout)
	}
}

func TestDetectionReadTimeout(t *testing.T) {
	if DetectionReadTimeout < 1*time.Second || DetectionReadTimeout > 30*time.Second {
		t.Errorf("DetectionReadTimeout = %v, expected between 1s and 30s", DetectionReadTimeout)
	}
}

func TestReaderWithStatsConcurrentAccess(t *testing.T) {
	mock := NewMockReader("/dev/ttyS1", make([]byte, 10000))
	reader := NewReaderWithStats(mock)

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 100)
			for j := 0; j < 10; j++ {
				reader.Read(buf)
			}
		}()
	}

	// Concurrent stat updates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				reader.LineRead()
				reader.IncrementErrors()
			}
		}()
	}

	// Concurrent stat reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				reader.Stats()
			}
		}()
	}

	wg.Wait()

	// Just verify we didn't panic or deadlock
	_, lines, errors := reader.Stats()
	if lines != 1000 {
		t.Errorf("LinesRead = %d, want 1000", lines)
	}
	if errors != 1000 {
		t.Errorf("Errors = %d, want 1000", errors)
	}
}

func BenchmarkReaderWithStatsRead(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}

	mock := &MockReader{
		device: "/dev/ttyS1",
		isOpen: true,
		data:   data,
	}
	reader := NewReaderWithStats(mock)

	buf := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.readIndex = 0 // Reset for each iteration
		reader.Read(buf)
	}
}
