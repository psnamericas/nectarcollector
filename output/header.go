package output

import (
	"fmt"
	"time"
)

// BuildHeader constructs a header in the format: [FIPSCODE][A1-16][YYYY-MM-DD HH:MM:SS.mmm]
func BuildHeader(fipsCode, aDesignation string, timestamp time.Time) string {
	// Format: [1429010002][A5][2025-12-03 15:04:05.123]
	return fmt.Sprintf("[%s][%s][%s] ",
		fipsCode,
		aDesignation,
		timestamp.Format("2006-01-02 15:04:05.000"))
}

// FormatTimestamp formats a timestamp in the required format with milliseconds
func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05.000")
}
