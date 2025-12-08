package serial

import (
	"testing"
)

func TestCountValidASCII(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{
			name:  "all printable",
			input: []byte("Hello World"),
			want:  11,
		},
		{
			name:  "with newlines",
			input: []byte("Line1\nLine2\r\nLine3"),
			want:  18,
		},
		{
			name:  "with tabs",
			input: []byte("col1\tcol2\tcol3"),
			want:  14,
		},
		{
			name:  "empty",
			input: []byte{},
			want:  0,
		},
		{
			name:  "all control chars",
			input: []byte{0x00, 0x01, 0x02, 0x03, 0x04},
			want:  0,
		},
		{
			name:  "mixed valid and invalid",
			input: []byte{0x41, 0x00, 0x42, 0x01, 0x43}, // A, NUL, B, SOH, C
			want:  3,
		},
		{
			name:  "high ascii (invalid)",
			input: []byte{0x80, 0x90, 0xA0, 0xFF},
			want:  0,
		},
		{
			name:  "boundary chars",
			input: []byte{0x1F, 0x20, 0x7E, 0x7F}, // below space, space, tilde, DEL
			want:  2,                              // space and tilde
		},
		{
			name:  "typical CDR line",
			input: []byte("[1234567890][A1][2025-01-01 12:00:00.000] CALL_START,12345,9876543210"),
			want:  69,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countValidASCII(tt.input)
			if got != tt.want {
				t.Errorf("countValidASCII() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCountValidASCIIRatio(t *testing.T) {
	// Test that at correct baud rate, typical text is ~95%+ valid
	goodText := []byte("This is a typical line of text with numbers 12345 and punctuation!?\n")
	validCount := countValidASCII(goodText)
	ratio := float64(validCount) / float64(len(goodText))

	if ratio < 0.95 {
		t.Errorf("Good text validity ratio = %.2f, expected >= 0.95", ratio)
	}

	// Test that garbage at wrong baud rate would be much lower
	// Simulated "wrong baud" data - random-ish bytes
	badData := []byte{0x82, 0x93, 0xA1, 0xB2, 0xC3, 0x01, 0x02, 0x8F, 0x9E, 0xAD}
	badValidCount := countValidASCII(badData)
	badRatio := float64(badValidCount) / float64(len(badData))

	if badRatio > 0.5 {
		t.Errorf("Bad data validity ratio = %.2f, expected < 0.5", badRatio)
	}
}

func TestValidityThreshold(t *testing.T) {
	// Verify the threshold constant is reasonable
	if ValidityThreshold < 0.7 || ValidityThreshold > 0.95 {
		t.Errorf("ValidityThreshold = %.2f, expected between 0.7 and 0.95", ValidityThreshold)
	}
}

func TestDetectorConstants(t *testing.T) {
	// Verify detection constants are reasonable
	if DetectionSettlingTime.Milliseconds() < 50 || DetectionSettlingTime.Milliseconds() > 500 {
		t.Errorf("DetectionSettlingTime = %v, expected between 50ms and 500ms", DetectionSettlingTime)
	}

	if DetectionBufferSize < 1024 || DetectionBufferSize > 65536 {
		t.Errorf("DetectionBufferSize = %d, expected between 1KB and 64KB", DetectionBufferSize)
	}

	if DetectionPollInterval.Milliseconds() < 1 || DetectionPollInterval.Milliseconds() > 100 {
		t.Errorf("DetectionPollInterval = %v, expected between 1ms and 100ms", DetectionPollInterval)
	}
}

func BenchmarkCountValidASCII(b *testing.B) {
	// Typical CDR line
	data := []byte("[1234567890][A1][2025-01-01 12:00:00.000] CALL_START,PSAP-01,5551234567,5559876543,2025-01-01T12:00:00Z,ANSWERED,120")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		countValidASCII(data)
	}
}

func BenchmarkCountValidASCIILarge(b *testing.B) {
	// Large buffer like during detection
	data := make([]byte, DetectionBufferSize)
	for i := range data {
		data[i] = byte('A' + (i % 26)) // Fill with letters
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		countValidASCII(data)
	}
}
