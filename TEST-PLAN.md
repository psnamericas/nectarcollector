# NectarCollector Test Plan

## Test Environment

**Topology:** Y-Cable Configuration (Single Sender → Two Collectors)

```
                    ┌─────────────────────┐
                    │   Sender (Source)   │
                    │  Generates CDR Data │
                    └──────────┬──────────┘
                               │
                         [Y-Cable Split]
                               │
                ┌──────────────┴──────────────┐
                │                             │
                ▼                             ▼
    ┌─────────────────────┐      ┌─────────────────────┐
    │  Collector A         │      │  Collector B         │
    │  100.104.43.22       │      │  100.73.92.66        │
    │  FIPS: 1429010003-A1 │      │  FIPS: 1429010002-A5 │
    │  HoneyView: :8080    │      │  HoneyView: :8080    │
    └─────────────────────┘      └─────────────────────┘
```

## Pre-Test Baseline

### Verify Normal Operation
- [ ] Both collectors receiving data
- [ ] Both HoneyView dashboards accessible
- [ ] NATS connected on both systems
- [ ] No errors in logs
- [ ] Line counts incrementing

**Commands:**
```bash
# Collector A (100.104.43.22)
curl -s http://100.104.43.22:8080/api/stats | python3 -m json.tool
tail -5 /var/log/nectarcollector/1429010003-A1.log

# Collector B (100.73.92.66)
curl -s http://100.73.92.66:8080/api/stats | python3 -m json.tool
tail -5 /var/log/nectarcollector/1429010002-A5.log
```

---

## Test Scenarios

### Test 1: Sender Disconnection (Y-Cable Unplug at Source)

**Purpose:** Verify both collectors detect loss of data and handle reconnection

**Steps:**
1. Record baseline stats from both collectors
2. Unplug Y-cable from sender (source side)
3. Wait 30 seconds
4. Check both collector states
5. Reconnect Y-cable to sender
6. Wait 60 seconds for reconnection
7. Verify both collectors resume capturing

**Expected Results:**
- Both collectors should:
  - Detect no data within detection timeout (~5 seconds)
  - Enter "reconnecting" state
  - Log reconnection attempts with exponential backoff
  - Automatically reconnect when cable restored
  - Resume data capture at detected baud rate
  - Show no data loss (first line after reconnect is sequential)

**Verification Commands:**
```bash
# During disconnection
ssh root@100.104.43.22 "systemctl status nectarcollector --no-pager | grep -A 5 State"
ssh root@100.73.92.66 "systemctl status nectarcollector --no-pager | grep -A 5 State"

curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0].state'
curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0].state'

# After reconnection
ssh root@100.104.43.22 "journalctl -u nectarcollector -n 20 --no-pager | grep -E 'Detection|Port opened'"
ssh root@100.73.92.66 "journalctl -u nectarcollector -n 20 --no-pager | grep -E 'Detection|Port opened'"
```

**Pass Criteria:**
- ✅ Both collectors enter reconnecting state
- ✅ Both collectors automatically resume after reconnection
- ✅ No service crashes or restarts
- ✅ Error counts minimal (< 10)

---

### Test 2: Collector A Serial Cable Disconnect

**Purpose:** Verify single collector failure doesn't affect other collector

**Steps:**
1. Record baseline stats from both collectors
2. Unplug serial cable from Collector A (100.104.43.22) only
3. Wait 30 seconds
4. Verify Collector B still receiving data
5. Check Collector A state
6. Reconnect cable to Collector A
7. Verify Collector A resumes

**Expected Results:**
- Collector A:
  - Enters reconnecting state
  - Logs reconnection attempts
  - Resumes when cable reconnected
- Collector B:
  - Continues normal operation
  - No interruption in data flow
  - Line count continues incrementing

**Verification Commands:**
```bash
# Check Collector B is still working
watch -n 1 "curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0].stats.LinesRead'"

# Check Collector A state
curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0] | {state, errors: .stats.Errors}'
```

**Pass Criteria:**
- ✅ Collector B unaffected, continues capturing
- ✅ Collector A reconnects automatically
- ✅ No data corruption on either collector

---

### Test 3: Collector B Serial Cable Disconnect

**Purpose:** Same as Test 2 but for Collector B

**Steps:**
1. Record baseline stats from both collectors
2. Unplug serial cable from Collector B (100.73.92.66) only
3. Wait 30 seconds
4. Verify Collector A still receiving data
5. Check Collector B state
6. Reconnect cable to Collector B
7. Verify Collector B resumes

**Expected Results:**
- Collector B: Reconnects automatically
- Collector A: Continues normal operation

**Verification Commands:**
```bash
# Check Collector A is still working
watch -n 1 "curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0].stats.LinesRead'"

# Check Collector B state
curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0] | {state, errors: .stats.Errors}'
```

**Pass Criteria:**
- ✅ Collector A unaffected, continues capturing
- ✅ Collector B reconnects automatically
- ✅ No data corruption on either collector

---

### Test 4: Both Collectors Disconnect Simultaneously

**Purpose:** Verify both collectors can reconnect independently

**Steps:**
1. Record baseline stats
2. Unplug both serial cables simultaneously
3. Wait 30 seconds
4. Check both collector states
5. Reconnect Collector A cable first
6. Wait 30 seconds
7. Reconnect Collector B cable
8. Verify both resume

**Expected Results:**
- Both enter reconnecting state
- Collector A resumes first
- Collector B resumes after its cable reconnected
- Both show same data capture (Y-cable split)

**Verification Commands:**
```bash
# Check both states
curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0].state'
curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0].state'

# Compare line counts (should be similar)
curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0].stats.LinesRead'
curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0].stats.LinesRead'
```

**Pass Criteria:**
- ✅ Both collectors reconnect independently
- ✅ Line counts within ±5 of each other
- ✅ No service crashes

---

### Test 5: NATS Service Failure (Collector A)

**Purpose:** Verify collector continues logging to file when NATS fails

**Steps:**
1. Record baseline stats for Collector A
2. Stop NATS service on Collector A: `ssh root@100.104.43.22 "systemctl stop nats"`
3. Wait 30 seconds
4. Verify collector still capturing to log file
5. Check HoneyView dashboard shows NATS disconnected
6. Restart NATS: `ssh root@100.104.43.22 "systemctl start nats"`
7. Verify NATS reconnects

**Expected Results:**
- Collector continues writing to log file
- NATS marked as disconnected in dashboard
- Warning logs about NATS publish failures
- Automatic reconnection when NATS restored
- No data loss (logs continue)

**Verification Commands:**
```bash
# Check NATS status in dashboard
curl -s http://100.104.43.22:8080/api/stats | jq '.nats_connected'

# Verify file logging continues
ssh root@100.104.43.22 "tail -5 /var/log/nectarcollector/1429010003-A1.log"

# Check for NATS warnings
ssh root@100.104.43.22 "journalctl -u nectarcollector -n 20 --no-pager | grep NATS"
```

**Pass Criteria:**
- ✅ File logging uninterrupted
- ✅ Dashboard shows NATS disconnected
- ✅ NATS reconnects automatically
- ✅ No data loss

---

### Test 6: NectarCollector Service Restart (Collector B)

**Purpose:** Verify clean restart and state recovery

**Steps:**
1. Record baseline stats for Collector B
2. Restart service: `ssh root@100.73.92.66 "systemctl restart nectarcollector"`
3. Wait 10 seconds for startup
4. Verify service is running
5. Check autobaud detection completes
6. Verify data capture resumes

**Expected Results:**
- Clean shutdown of previous process
- Successful restart
- Autobaud re-detection
- Data capture resumes
- HoneyView dashboard accessible

**Verification Commands:**
```bash
# Check service status
ssh root@100.73.92.66 "systemctl status nectarcollector --no-pager"

# Check detection logs
ssh root@100.73.92.66 "journalctl -u nectarcollector --since '1 minute ago' --no-pager | grep -E 'detect|baud|Port opened'"

# Verify dashboard
curl -s http://100.73.92.66:8080/api/health
```

**Pass Criteria:**
- ✅ Service restarts cleanly
- ✅ Autobaud detection succeeds
- ✅ Data capture resumes within 30 seconds
- ✅ Dashboard accessible

---

### Test 7: Baud Rate Change at Sender

**Purpose:** Verify autobaud re-detection on sender change

**Steps:**
1. Note current baud rate on both collectors
2. Change sender baud rate (if possible)
3. Unplug and replug Y-cable (to trigger re-detection)
4. Verify both collectors detect new baud rate

**Expected Results:**
- Both collectors run autobaud detection
- New baud rate detected correctly
- Data capture resumes at new rate

**Verification Commands:**
```bash
# Check detected baud rates
curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0].stats.DetectedBaud'
curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0].stats.DetectedBaud'
```

**Pass Criteria:**
- ✅ New baud rate detected
- ✅ Data capture resumes
- ✅ Both collectors show same baud rate

---

### Test 8: Simultaneous Power Loss and Recovery

**Purpose:** Verify system recovery from complete power failure

**Steps:**
1. Record current state
2. Simulate power loss: `ssh root@100.104.43.22 "systemctl stop nats nectarcollector"`
3. Wait 30 seconds
4. Restore power: `ssh root@100.104.43.22 "systemctl start nats && sleep 2 && systemctl start nectarcollector"`
5. Wait for startup
6. Verify services auto-start
7. Check data capture resumes

**Expected Results:**
- Services configured to start on boot
- NATS starts first, then nectarcollector
- Autobaud detection runs
- Data capture resumes

**Verification Commands:**
```bash
# Check service auto-start configuration
ssh root@100.104.43.22 "systemctl is-enabled nats nectarcollector"

# Check service status
ssh root@100.104.43.22 "systemctl status nats nectarcollector --no-pager"
```

**Pass Criteria:**
- ✅ Both services auto-start
- ✅ Data capture resumes automatically
- ✅ No manual intervention required

---

### Test 9: Log File Rotation

**Purpose:** Verify log rotation works correctly

**Steps:**
1. Check current log file size
2. If < 100MB, artificially trigger rotation (optional)
3. Verify new log file created
4. Check data continues to new file
5. Verify old file compressed (if configured)

**Verification Commands:**
```bash
# Check log file size
ssh root@100.104.43.22 "ls -lh /var/log/nectarcollector/"

# Check lumberjack rotation (happens automatically at 100MB)
ssh root@100.104.43.22 "ls -lh /var/log/nectarcollector/*.gz 2>/dev/null || echo 'No rotated logs yet'"
```

**Pass Criteria:**
- ✅ Files rotate at 100MB
- ✅ Old files compressed
- ✅ Max 10 backups kept
- ✅ No data loss during rotation

---

### Test 10: High Data Volume Stress Test

**Purpose:** Verify collectors handle sustained high-volume data

**Steps:**
1. Monitor both collectors for 1 hour
2. Check error counts
3. Verify line counts incrementing steadily
4. Check memory usage stable
5. Check CPU usage reasonable

**Verification Commands:**
```bash
# Monitor line counts
watch -n 5 "curl -s http://100.104.43.22:8080/api/stats | jq '.channels[0].stats.LinesRead' && \
             curl -s http://100.73.92.66:8080/api/stats | jq '.channels[0].stats.LinesRead'"

# Check resource usage
ssh root@100.104.43.22 "systemctl status nectarcollector --no-pager | grep -E 'Memory|CPU'"
ssh root@100.73.92.66 "systemctl status nectarcollector --no-pager | grep -E 'Memory|CPU'"
```

**Pass Criteria:**
- ✅ Error counts < 0.1% of line counts
- ✅ Memory usage < 50MB
- ✅ CPU usage < 5%
- ✅ No service crashes

---

### Test 11: HoneyView Dashboard Stress Test

**Purpose:** Verify dashboard handles multiple concurrent users

**Steps:**
1. Open HoneyView on both collectors in multiple browser tabs
2. Start live feed on all tabs
3. Monitor for 5 minutes
4. Check for lag or errors

**Verification Commands:**
```bash
# Monitor HTTP connections
ssh root@100.104.43.22 "netstat -an | grep :8080 | wc -l"
ssh root@100.73.92.66 "netstat -an | grep :8080 | wc -l"
```

**Pass Criteria:**
- ✅ Dashboard responsive
- ✅ Live feeds update smoothly
- ✅ No memory leaks
- ✅ No connection errors

---

## Test Data Collection

For each test, record:

| Test # | Collector | Start Time | End Time | State Before | State After | Errors | Lines Before | Lines After | Notes |
|--------|-----------|------------|----------|--------------|-------------|--------|--------------|-------------|-------|
| 1      | A         |            |          |              |             |        |              |             |       |
| 1      | B         |            |          |              |             |        |              |             |       |
| 2      | A         |            |          |              |             |        |              |             |       |
| 2      | B         |            |          |              |             |        |              |             |       |
| ...    | ...       |            |          |              |             |        |              |             |       |

---

## Automated Test Script

```bash
#!/bin/bash
# test-nectarcollector.sh
# Automated test script for NectarCollector dual-collector setup

COLLECTOR_A="100.104.43.22"
COLLECTOR_B="100.73.92.66"

echo "=== NectarCollector Test Suite ==="
echo "Collector A: $COLLECTOR_A"
echo "Collector B: $COLLECTOR_B"
echo ""

# Test 1: Check both collectors are running
echo "Test 1: Verify both collectors operational"
curl -s http://$COLLECTOR_A:8080/api/health | jq -r '.status'
curl -s http://$COLLECTOR_B:8080/api/health | jq -r '.status'

# Test 2: Compare line counts
echo ""
echo "Test 2: Compare line counts (should be within ±10)"
LINES_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq '.channels[0].stats.LinesRead')
LINES_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq '.channels[0].stats.LinesRead')
DIFF=$((LINES_A - LINES_B))
echo "Collector A: $LINES_A lines"
echo "Collector B: $LINES_B lines"
echo "Difference: $DIFF lines"

# Test 3: Check NATS connectivity
echo ""
echo "Test 3: NATS connectivity"
curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '"Collector A NATS: " + (.nats_connected|tostring)'
curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '"Collector B NATS: " + (.nats_connected|tostring)'

# Test 4: Check error counts
echo ""
echo "Test 4: Error counts (should be 0 or very low)"
curl -s http://$COLLECTOR_A:8080/api/stats | jq '"Collector A Errors: " + (.channels[0].stats.Errors|tostring)'
curl -s http://$COLLECTOR_B:8080/api/stats | jq '"Collector B Errors: " + (.channels[0].stats.Errors|tostring)'

echo ""
echo "=== Test Suite Complete ==="
```

---

## Expected Baseline Metrics

**Normal Operation:**
- State: `running`
- Error Count: `0`
- NATS Connected: `true`
- Line Count Difference: `< ±10 lines`
- Memory Usage: `< 10MB per collector`
- CPU Usage: `< 2%`

**During Reconnection:**
- State: `reconnecting`
- Error Count: `< 10`
- Reconnection Delay: `5s, 10s, 20s, 40s... (exponential backoff)`

**Post-Recovery:**
- State: `running`
- Detection Time: `< 30 seconds`
- Data Loss: `0 lines` (both collectors should show similar counts)

---

## Troubleshooting Guide

### Collector Not Receiving Data
1. Check cable connections
2. Verify sender is transmitting
3. Check serial port permissions: `ls -l /dev/ttyS4`
4. Check autobaud detection logs
5. Verify baud rate matches sender

### NATS Connection Failed
1. Check NATS service: `systemctl status nats`
2. Verify port 4222 accessible: `netstat -an | grep 4222`
3. Check NATS logs: `journalctl -u nats -n 50`
4. Restart NATS: `systemctl restart nats`

### High Error Counts
1. Check for hardware issues (loose cables)
2. Verify baud rate correct
3. Check for electrical noise/interference
4. Review error logs: `journalctl -u nectarcollector -n 100`

### Dashboard Not Accessible
1. Check nectarcollector service running
2. Verify port 8080 accessible: `netstat -an | grep 8080`
3. Check firewall rules
4. Test API directly: `curl http://localhost:8080/api/health`

---

## Success Criteria Summary

All tests must meet these criteria:
- ✅ No service crashes or panics
- ✅ Automatic recovery from all failure scenarios
- ✅ Data consistency between both collectors (within ±10 lines)
- ✅ Error rates < 0.1%
- ✅ Resource usage remains stable
- ✅ Dashboard remains accessible throughout tests
- ✅ No manual intervention required for recovery

---

## Test Schedule Recommendation

**Phase 1: Basic Functionality** (Day 1)
- Tests 1, 2, 3, 6

**Phase 2: Advanced Failure Scenarios** (Day 2)
- Tests 4, 5, 7, 8

**Phase 3: Performance & Endurance** (Day 3-4)
- Tests 9, 10, 11

**Phase 4: Long-Term Stability** (Week 1)
- 24-hour continuous monitoring
- Weekly log review
- Monthly maintenance verification
