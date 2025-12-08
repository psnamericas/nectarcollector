package output

import (
	"testing"
	"time"
)

func TestBuildHeader(t *testing.T) {
	// Fixed timestamp for reproducible tests
	ts := time.Date(2025, 12, 3, 15, 4, 5, 123000000, time.UTC)

	tests := []struct {
		name         string
		fipsCode     string
		aDesignation string
		want         string
	}{
		{
			name:         "standard format",
			fipsCode:     "1429010002",
			aDesignation: "A5",
			want:         "[1429010002][A5][2025-12-03 15:04:05.123] ",
		},
		{
			name:         "A1 designation",
			fipsCode:     "3100000001",
			aDesignation: "A1",
			want:         "[3100000001][A1][2025-12-03 15:04:05.123] ",
		},
		{
			name:         "A16 designation",
			fipsCode:     "9999999999",
			aDesignation: "A16",
			want:         "[9999999999][A16][2025-12-03 15:04:05.123] ",
		},
		{
			name:         "empty fips code",
			fipsCode:     "",
			aDesignation: "A1",
			want:         "[][A1][2025-12-03 15:04:05.123] ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildHeader(tt.fipsCode, tt.aDesignation, ts)
			if got != tt.want {
				t.Errorf("BuildHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildHeaderMilliseconds(t *testing.T) {
	// Test that milliseconds are properly formatted
	tests := []struct {
		name   string
		millis int
		want   string
	}{
		{"zero millis", 0, ".000"},
		{"single digit", 1000000, ".001"},
		{"double digit", 10000000, ".010"},
		{"triple digit", 100000000, ".100"},
		{"max", 999000000, ".999"},
		{"mid value", 456000000, ".456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := time.Date(2025, 1, 1, 0, 0, 0, tt.millis, time.UTC)
			header := BuildHeader("1234567890", "A1", ts)
			if !containsSubstring(header, tt.want) {
				t.Errorf("BuildHeader() = %q, want substring %q", header, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	ts := time.Date(2025, 12, 3, 15, 4, 5, 123000000, time.UTC)
	want := "2025-12-03 15:04:05.123"
	got := FormatTimestamp(ts)
	if got != want {
		t.Errorf("FormatTimestamp() = %q, want %q", got, want)
	}
}

func TestFormatTimestampEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		ts   time.Time
		want string
	}{
		{
			name: "midnight",
			ts:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "2025-01-01 00:00:00.000",
		},
		{
			name: "end of day",
			ts:   time.Date(2025, 12, 31, 23, 59, 59, 999000000, time.UTC),
			want: "2025-12-31 23:59:59.999",
		},
		{
			name: "leap year",
			ts:   time.Date(2024, 2, 29, 12, 30, 45, 500000000, time.UTC),
			want: "2024-02-29 12:30:45.500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimestamp(tt.ts)
			if got != tt.want {
				t.Errorf("FormatTimestamp() = %q, want %q", got, tt.want)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func BenchmarkBuildHeader(b *testing.B) {
	ts := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildHeader("1429010002", "A5", ts)
	}
}
