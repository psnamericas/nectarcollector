# Serial Communication Hardening Tasks

This document tracks the work needed to make NectarCollector's serial handling rock-solid, emulating the reliability of dedicated hardware serial capture devices like Scannex boxes.

## Goal

Create a serial capture system that can run unattended for years without intervention, surviving:
- Cable disconnections and reconnections
- Device power cycles
- USB adapter resets
- Electrical noise and interference
- System resource pressure
- Graceful and ungraceful shutdowns

---

## Critical Priority

### [x] 1. Fix Race Condition in Read() Method
**File:** `serial/reader.go:89-98`

**Problem:** The mutex is released before `port.Read()` is called, allowing `Close()` to invalidate the port reference mid-operation.

**Current Code:**
```go
func (r *RealReader) Read(p []byte) (n int, err error) {
    r.mu.Lock()
    port := r.port
    r.mu.Unlock()  // Released too early

    if port == nil {
        return 0, fmt.Errorf("port not open")
    }

    return port.Read(p)  // Race: port could be closed here
}
```

**Solution:** Use `sync.RWMutex` - Read takes RLock, Close takes full Lock. This allows concurrent reads while ensuring Close waits for all reads to complete.

---

### [x] 2. Add Serial Port Drain Before Close
**File:** `serial/reader.go:102-115`

**Problem:** Closing without draining can lose data in TX buffer and leave stale data in RX buffer.

**Solution:** Call `port.Drain()` before `Close()`, and optionally `ResetInputBuffer()` / `ResetOutputBuffer()`.

---

### [x] 3. Fix Scanner Blocking Graceful Shutdown
**File:** `capture/channel.go:261-287`

**Problem:** `scanner.Scan()` blocks for up to 5 seconds (read timeout), preventing timely shutdown response.

**Solution Options:**
1. Use shorter read timeouts with explicit timeout handling
2. Use a dedicated reader goroutine with channel-based communication
3. Set a deadline on the underlying port before shutdown

---

## High Priority

### [x] 4. Fix Flow Control Configuration
**File:** `serial/reader.go:73-80`

**Problem:** The flow control code doesn't actually enable RTS/CTS - it just re-applies the same Mode.

**Solution:** Use `port.SetRTS()` and monitor CTS via `GetModemStatusBits()`, or investigate platform-specific flow control APIs.

---

### [x] 5. Flush Input Buffer During Baud Rate Detection
**File:** `serial/detection.go:46-53`

**Problem:** Stale data from previous baud rate test contaminates the next test's ASCII ratio calculation.

**Solution:** Call `port.ResetInputBuffer()` after opening at each new baud rate.

---

### [x] 6. Add Delay Between Detection Cycles
**File:** `serial/detection.go:53`

**Problem:** Rapid open/close cycles can confuse USB-to-serial adapters. RS-232 spec recommends settling time.

**Solution:** Add 50-100ms delay between closing one baud rate test and opening the next.

---

### [x] 7. Fix Reconfigure TOCTOU Race
**File:** `serial/reader.go:130-139`

**Problem:** `baudRate` and `useFlowControl` are modified outside mutex protection.

**Solution:** Hold mutex during entire reconfigure operation, or use atomic operations.

---

## Medium Priority

### [x] 8. Make Parity Configurable
**File:** `serial/reader.go:58`

**Problem:** Hardcoded `NoParity` won't work with industrial devices expecting even/odd parity.

**Solution:** Add parity to config and Mode struct.

---

### [x] 9. Expand Valid ASCII Character Set
**File:** `serial/detection.go:174-184`

**Problem:** Missing NUL, ESC, BS, FF, and extended ASCII that some protocols use.

**Solution:** Make character set configurable or expand defaults.

---

### [x] 10. Extract Magic Numbers to Constants/Config
**Files:** Various

**Values to extract:**
- `5 * time.Second` - Read timeout
- `64*1024` / `1024*1024` - Buffer sizes
- `4096` - Detection buffer
- `0.80` - ASCII validity threshold
- `10 * time.Millisecond` - Poll sleep
- `50` - Minimum bytes for valid detection

---

### [x] 11. Add Modem Status Line Monitoring
**File:** New functionality

**Problem:** No detection of cable disconnect via DCD/DSR signals.

**Solution:** Periodically poll `GetModemStatusBits()` and trigger reconnect on carrier loss.

---

### [x] 12. Prevent File Descriptor Leaks
**File:** `capture/channel.go:238-257`

**Problem:** Resources could leak if failure occurs between open and deferred close.

**Solution:** Use consistent `defer reader.Close()` pattern immediately after successful open.

---

## Low Priority / Enhancements

### [ ] 13. Add Serial Break Detection
Detect break conditions for protocols that use them.

### [ ] 14. Enhanced Statistics Tracking
Add: framing errors, parity errors, buffer overruns, time since last data.

### [ ] 15. Improve Detection Sampling
Use accumulated sampling window instead of single Read() calls.

---

## Research Notes

### Scannex ip.buffer Reference (1978-2024, RIP)

Scannex was the gold standard for serial data capture for nearly 50 years. Their ip.buffer devices were used in telecoms, utilities, and oceanographic applications where reliability was paramount. Key features we should emulate:

#### Auto DTE/DCE Detection
- Continuously monitors RS232 signal levels on pins 2 & 3
- Automatically configures for DTE or DCE based on detected signals
- COM port stays OFF until valid connection recognized (prevents garbage)
- Can detect "two transmit lines" scenario (Y-lead parallel logging)
- Falls back to standard PC pinout (DTE) when ambiguous
- Supports forcing DTE/DCE for non-compliant devices

#### Autobaud Detection
- Strongly recommended to leave enabled for PBX/CDR collection
- "Quickly locks onto correct baud rate" if settings change
- Maintains synchronization even if source speed/format changes mid-stream
- Works with both ASCII and binary data sources

#### Hardware Reliability
- Non-volatile flash storage (10-year data retention without power)
- Line-powered operation (MicroBuffer can power from the serial line itself)
- Survives engineer configuration changes automatically
- Designed for "set and forget" deployment

#### Signal Handling
- Detects when PBX doesn't turn on COM until active connection present
- Handles non-standard RS232 levels (positive-only voltages)
- Source status indicator (GREEN = connected, RED = problem)
- RS232 disconnect alerting capability

#### Key Design Principles from Scannex
1. **Never lose data** - Large buffers, non-volatile storage
2. **Self-healing** - Auto-detect everything possible, recover from changes
3. **Minimal configuration** - Works with "standard pin-for-pin cables"
4. **Long-term unattended** - Years of operation without intervention
5. **Graceful degradation** - Continue logging even with partial failures

Sources:
- [Scannex Archive](https://www.scannex.co.uk/) - Company closed after ~50 years
- [Scannex Products](https://scannex.com/products/)
- [ip.buffer Specifications](https://www.scannex.com/products/ipbuffer/specifications.html)
- [Comm One Scannex Setup Guide](https://www.commone.com/faq-1696-scannex-buffer-setup)

---

## Progress Log

| Date | Task | Status | Notes |
|------|------|--------|-------|
| 2025-12-04 | 1-12 | Complete | All critical, high, and medium priority tasks implemented |
| 2025-12-04 | Research | Complete | Scannex ip.buffer best practices documented |
| 2025-12-04 | Simplify | Complete | Removed over-engineered countValidASCII, updated log defaults |
| 2025-12-04 | Build | Pass | `go build` and `go vet` pass cleanly |

---

## Testing Checklist

Before considering hardening complete:

- [ ] 24-hour continuous run with active serial data
- [ ] Cable disconnect/reconnect during operation
- [ ] USB adapter unplug/replug recovery
- [ ] Graceful shutdown completes in <5 seconds
- [ ] No file descriptor leaks over 1000 reconnect cycles
- [ ] Memory stable over 7-day run
- [ ] All baud rates detect correctly (300-115200)
- [ ] Works with both null modem and straight-through cables
