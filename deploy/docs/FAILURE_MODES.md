# NectarCollector Failure Modes & Recovery

```
         _
        /_/_      .'''.
     =O(_)))) ~=='  _  '=~    "When things go wrong, we keep buzzing"
        \_\       /  \
                  \__/
```

This document analyzes all failure scenarios and how NectarCollector handles them.

---

## Summary Matrix

| Failure | Data Loss Risk | Recovery | Current Status |
|---------|----------------|----------|----------------|
| Serial cable disconnect | None | Auto-reconnect with backoff | HANDLED |
| USB adapter unplug | None | Auto-reconnect with backoff | HANDLED |
| Serial port disappears | None | Auto-reconnect with backoff | HANDLED |
| Baud rate mismatch | None | Auto-detection retries | HANDLED |
| NATS down | None | Logs to file, NATS reconnects | HANDLED |
| NATS slow/backpressure | Possible | Needs work | GAP |
| Disk full | Yes | Lumberjack rotation | PARTIAL |
| Process crash | Brief gap | Systemd restarts | HANDLED |
| OOM kill | Brief gap | Systemd restarts | HANDLED |
| Power loss | Brief gap | Systemd auto-start | HANDLED |
| Config error | Won't start | Logs error, exits | HANDLED |
| Network loss | None (local) | Tailscale reconnects | HANDLED |
| JetStream storage full | Possible | Needs monitoring | GAP |

---

## 1. Serial Port Failures

### 1.1 Cable Disconnect / USB Unplug

**Scenario:** Physical cable pulled, USB adapter removed, or port disappears.

**Current Behavior:**
```
channel.go:readLoop()
  → scanner.Scan() fails with error
  → Returns error to captureLoop()
  → setState(StateReconnecting)
  → handleReconnect() with exponential backoff
  → Loop back to runCaptureSession()
  → Re-detect baud rate and reconnect
```

**Recovery:** Automatic, with exponential backoff (5s → 10s → 20s → ... → 300s max)

**Data Loss:** None. Data just stops flowing until reconnection.

**Status:** HANDLED

### 1.2 Baud Rate Change

**Scenario:** Source device (PBX/VIPER) reboots with different baud rate.

**Current Behavior:**
- If `baud_rate: 0` in config, auto-detection runs on each reconnect
- Tries all configured baud rates until valid ASCII stream found
- 80% printable ASCII threshold with 50+ bytes

**Status:** HANDLED

### 1.3 Garbage Data / Line Noise

**Scenario:** Electrical interference corrupts serial data.

**Current Behavior:**
- Scanner reads lines until newline
- Invalid data passes through and gets logged (garbage in, garbage out)
- Detection phase validates ASCII ratio

**Potential Issue:** Extended garbage could fill logs with junk.

**Status:** HANDLED (data integrity is source's responsibility)

---

## 2. NATS Failures

### 2.1 NATS Server Down at Startup

**Scenario:** NectarCollector starts but NATS isn't running yet.

**Current Behavior:**
```go
// manager.go:36-46
natsConn, err := output.NewNATSConnection(...)
if err != nil {
    m.logger.Warn("Failed to connect to NATS, continuing without NATS", "error", err)
    // Continue without NATS - log files will still work
}
```

**Recovery:** Logs warning, continues with file-only capture.

**Data Loss:** NATS messages lost, but files have everything.

**Status:** HANDLED (graceful degradation)

### 2.2 NATS Server Crashes Mid-Operation

**Scenario:** NATS was running, then dies.

**Current Behavior:**
```go
// output/dual.go:132-146
opts := []nats.Option{
    nats.MaxReconnects(maxReconnects),  // Configurable, default 10
    nats.ReconnectHandler(...),
    nats.DisconnectErrHandler(...),
}
```

**Recovery:** NATS client auto-reconnects up to MaxReconnects times.

**Data Loss:** Messages during disconnect are lost to NATS, but logged to file.

**Status:** HANDLED

### 2.3 NATS Publish Fails (Backpressure)

**Scenario:** NATS server is slow, publish blocks or fails.

**Current Behavior:**
```go
// output/dual.go:87-98
if err := dw.natsConn.Publish(dw.natsSubject, []byte(data)); err != nil {
    dw.logger.Warn("Failed to publish to NATS", ...)
    // Continue - don't block on NATS
}
```

**Potential Issue:** `Publish()` is async but can still block if buffer full. If NATS is persistently slow, could cause goroutine backup.

**Status:** GAP - Should use async publish with timeout or check buffer status.

### 2.4 JetStream Storage Full

**Scenario:** 64GB JetStream storage fills up.

**Current Behavior:** Unknown - depends on stream config's `discard` policy.

**With `"discard": "new"`:** New messages rejected, data loss.
**With `"discard": "old"`:** Old messages dropped.

**Status:** GAP - Need monitoring/alerting for storage usage.

---

## 3. Disk / Filesystem Failures

### 3.1 Disk Full

**Scenario:** Log partition fills up.

**Current Behavior:**
- Lumberjack rotates files at 50MB
- Keeps 10 backups with gzip compression
- Total max: ~50MB + (10 × ~5MB compressed) ≈ 100MB per port

**Potential Issue:** If disk fills from other sources, writes fail silently.

**Status:** PARTIAL - Rotation works, but no disk space monitoring.

### 3.2 Log Directory Deleted

**Scenario:** Someone runs `rm -rf /var/log/nectarcollector`.

**Current Behavior:** Lumberjack will fail to write. Error logged (but where?).

**Status:** GAP - Should recreate directory or alert.

### 3.3 Permission Denied

**Scenario:** Permissions changed on log directory.

**Current Behavior:** Write fails, error logged.

**Status:** HANDLED (errors logged, service continues)

---

## 4. Process Failures

### 4.1 Process Crash (Panic)

**Scenario:** Unhandled panic crashes NectarCollector.

**Current Behavior:**
```ini
# nectarcollector.service
Restart=always
RestartSec=5
```

**Recovery:** Systemd restarts within 5 seconds.

**Data Loss:** Brief gap during restart + detection.

**Status:** HANDLED

### 4.2 OOM Kill

**Scenario:** System runs out of memory, OOM killer targets NectarCollector.

**Current Behavior:** Same as crash - systemd restarts.

**Potential Issue:** If OOM is persistent (memory leak), restart loop.

**Status:** HANDLED (but should monitor memory usage)

### 4.3 Graceful Shutdown

**Scenario:** `systemctl stop nectarcollector` or SIGTERM.

**Current Behavior:**
```go
// main.go:91
shutdownCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)
// Stops monitoring, then capture manager
// 30-second timeout before force exit
```

**Recovery:** Clean shutdown, files flushed, NATS drained.

**Status:** HANDLED

---

## 5. System Failures

### 5.1 Power Loss

**Scenario:** Box loses power unexpectedly.

**Current Behavior:**
- Systemd auto-starts services on boot
- `WantedBy=multi-user.target` ensures startup

**Data Loss:** Last partial line might be lost (no fsync on every write).

**Status:** HANDLED

### 5.2 Kernel Panic

**Scenario:** Linux kernel crashes.

**Current Behavior:** Same as power loss.

**Status:** HANDLED (hardware watchdog could help)

### 5.3 Time Jump (NTP Sync)

**Scenario:** System clock jumps forward/backward.

**Current Behavior:** Timestamps in CDR headers will reflect the jump.

**Status:** HANDLED (UTC timezone helps, data still valid)

---

## 6. Network Failures

### 6.1 Network Interface Down

**Scenario:** Ethernet cable unplugged.

**Current Behavior:**
- Local capture continues (serial → file + local NATS)
- Tailscale disconnects but auto-reconnects when network returns

**Status:** HANDLED

### 6.2 DNS Failure

**Scenario:** DNS not resolving.

**Current Behavior:**
- NATS uses `localhost:4222` by default (no DNS needed)
- Tailscale handles its own DNS

**Status:** HANDLED

### 6.3 Tailscale Auth Expires

**Scenario:** Tailscale key expires.

**Current Behavior:**
- SSH via Tailscale fails
- Fallback: Network SSH to `psna@<local-ip>`
- Fallback: Serial console on COM1
- Fallback: Physical HDMI + keyboard

**Status:** HANDLED (multiple fallbacks)

---

## 7. Configuration Failures

### 7.1 Invalid JSON Config

**Scenario:** Config file has syntax error.

**Current Behavior:**
```go
// config/config.go
cfg, err := config.Load(*configPath)
if err != nil {
    log.Fatalf("Failed to load configuration: %v", err)
}
```

**Recovery:** Exits with clear error message. Systemd won't restart (not a crash).

**Status:** HANDLED

### 7.2 Missing Serial Device

**Scenario:** Config references `/dev/ttyUSB0` but device doesn't exist.

**Current Behavior:**
- Detection/open fails
- Channel marked as error
- Other channels continue
- Exponential backoff retries (device might appear later)

**Status:** HANDLED

### 7.3 Invalid NATS URL

**Scenario:** NATS URL is wrong.

**Current Behavior:** Warning logged, continues with file-only capture.

**Status:** HANDLED

---

## Identified Gaps

### GAP 1: NATS Publish Backpressure

**Problem:** If NATS is slow, `Publish()` could block the capture goroutine.

**Fix:** Use async publish or check pending buffer:
```go
if dw.natsConn.Buffered() > MaxPendingBytes {
    // Skip NATS, just log
}
```

### GAP 2: JetStream Storage Monitoring

**Problem:** No visibility into JetStream storage usage.

**Fix:** Add `/api/health` check for NATS storage:
```go
// Check stream info
info, _ := js.StreamInfo("ne-cdr")
if info.State.Bytes > threshold {
    // Alert
}
```

### GAP 3: Disk Space Monitoring

**Problem:** No visibility into log disk usage.

**Fix:** Add disk space check to health endpoint:
```go
var stat syscall.Statfs_t
syscall.Statfs("/var/log/nectarcollector", &stat)
freeBytes := stat.Bavail * uint64(stat.Bsize)
```

### GAP 4: Log Directory Recreation

**Problem:** If log directory deleted, writes fail.

**Fix:** Check/create directory before each write or on rotation.

### GAP 5: Startup NATS Retry

**Problem:** If NATS is down at startup, we never retry connecting.

**Fix:** Background goroutine to periodically retry NATS connection:
```go
go func() {
    for m.natsConn == nil {
        time.Sleep(30 * time.Second)
        m.tryReconnectNATS()
    }
}()
```

---

## Recommendations

### Critical (Fix Now)
1. **GAP 5:** Add NATS retry on startup - currently if NATS starts after NectarCollector, it never connects.

### High Priority (Fix Soon)
2. **GAP 1:** Add NATS publish timeout/buffer check
3. **GAP 3:** Add disk space to health check

### Medium Priority (Monitor)
4. **GAP 2:** Add JetStream storage monitoring
5. **GAP 4:** Add log directory check/recreation

### Already Solid
- Serial reconnection with exponential backoff
- Graceful degradation (NATS down → file only)
- Systemd restart on crash
- 30-second graceful shutdown
- Multiple access fallbacks (Tailscale → SSH → Serial → Physical)

---

*Last updated: $(date)*
