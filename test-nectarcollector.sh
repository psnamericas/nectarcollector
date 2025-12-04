#!/bin/bash
# test-nectarcollector.sh
# Automated test script for NectarCollector dual-collector setup

set -e

COLLECTOR_A="100.104.43.22"
COLLECTOR_B="100.73.92.66"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

function pass() {
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

function fail() {
    echo -e "${RED}✗ FAIL${NC}: $1"
}

function warn() {
    echo -e "${YELLOW}⚠ WARN${NC}: $1"
}

function header() {
    echo ""
    echo "========================================"
    echo "$1"
    echo "========================================"
}

header "NectarCollector Test Suite"
echo "Collector A: $COLLECTOR_A"
echo "Collector B: $COLLECTOR_B"
echo "Timestamp: $(date)"

# Test 1: Health checks
header "Test 1: Health Checks"
HEALTH_A=$(curl -s http://$COLLECTOR_A:8080/api/health | jq -r '.status')
HEALTH_B=$(curl -s http://$COLLECTOR_B:8080/api/health | jq -r '.status')

if [ "$HEALTH_A" == "healthy" ]; then
    pass "Collector A is healthy"
else
    fail "Collector A health check failed: $HEALTH_A"
fi

if [ "$HEALTH_B" == "healthy" ]; then
    pass "Collector B is healthy"
else
    fail "Collector B health check failed: $HEALTH_B"
fi

# Test 2: Service status
header "Test 2: Service Status"
STATE_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '.channels[0].state')
STATE_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '.channels[0].state')

echo "Collector A state: $STATE_A"
echo "Collector B state: $STATE_B"

if [ "$STATE_A" == "running" ]; then
    pass "Collector A is running"
else
    warn "Collector A state: $STATE_A"
fi

if [ "$STATE_B" == "running" ]; then
    pass "Collector B is running"
else
    warn "Collector B state: $STATE_B"
fi

# Test 3: NATS connectivity
header "Test 3: NATS Connectivity"
NATS_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '.nats_connected')
NATS_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '.nats_connected')

if [ "$NATS_A" == "true" ]; then
    pass "Collector A NATS connected"
else
    fail "Collector A NATS disconnected"
fi

if [ "$NATS_B" == "true" ]; then
    pass "Collector B NATS connected"
else
    fail "Collector B NATS disconnected"
fi

# Test 4: Data capture comparison
header "Test 4: Data Capture Comparison"
LINES_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq '.channels[0].stats.LinesRead')
LINES_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq '.channels[0].stats.LinesRead')
BYTES_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq '.channels[0].stats.BytesRead')
BYTES_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq '.channels[0].stats.BytesRead')

echo "Collector A: $LINES_A lines, $BYTES_A bytes"
echo "Collector B: $LINES_B lines, $BYTES_B bytes"

DIFF=$((LINES_A - LINES_B))
ABS_DIFF=${DIFF#-}  # Absolute value

if [ "$ABS_DIFF" -le 10 ]; then
    pass "Line counts within tolerance (diff: $DIFF)"
else
    warn "Line counts differ significantly (diff: $DIFF)"
fi

# Test 5: Error counts
header "Test 5: Error Counts"
ERRORS_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq '.channels[0].stats.Errors')
ERRORS_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq '.channels[0].stats.Errors')

echo "Collector A errors: $ERRORS_A"
echo "Collector B errors: $ERRORS_B"

if [ "$ERRORS_A" -eq 0 ]; then
    pass "Collector A has no errors"
elif [ "$ERRORS_A" -lt 10 ]; then
    warn "Collector A has $ERRORS_A errors (low)"
else
    fail "Collector A has $ERRORS_A errors (high)"
fi

if [ "$ERRORS_B" -eq 0 ]; then
    pass "Collector B has no errors"
elif [ "$ERRORS_B" -lt 10 ]; then
    warn "Collector B has $ERRORS_B errors (low)"
else
    fail "Collector B has $ERRORS_B errors (high)"
fi

# Test 6: Baud rate detection
header "Test 6: Baud Rate Detection"
BAUD_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq '.channels[0].stats.DetectedBaud')
BAUD_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq '.channels[0].stats.DetectedBaud')

echo "Collector A baud: $BAUD_A"
echo "Collector B baud: $BAUD_B"

if [ "$BAUD_A" == "$BAUD_B" ]; then
    pass "Baud rates match ($BAUD_A)"
else
    fail "Baud rates differ (A: $BAUD_A, B: $BAUD_B)"
fi

# Test 7: Recent data activity
header "Test 7: Recent Data Activity"
LAST_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '.channels[0].stats.LastLineTime')
LAST_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '.channels[0].stats.LastLineTime')

echo "Collector A last line: $LAST_A"
echo "Collector B last line: $LAST_B"

# Calculate age in seconds (rough check)
NOW=$(date +%s)
LAST_A_SEC=$(date -d "$LAST_A" +%s 2>/dev/null || echo 0)
LAST_B_SEC=$(date -d "$LAST_B" +%s 2>/dev/null || echo 0)

AGE_A=$((NOW - LAST_A_SEC))
AGE_B=$((NOW - LAST_B_SEC))

if [ "$AGE_A" -lt 60 ]; then
    pass "Collector A received data recently (${AGE_A}s ago)"
elif [ "$AGE_A" -lt 300 ]; then
    warn "Collector A last data ${AGE_A}s ago"
else
    fail "Collector A no recent data (${AGE_A}s ago)"
fi

if [ "$AGE_B" -lt 60 ]; then
    pass "Collector B received data recently (${AGE_B}s ago)"
elif [ "$AGE_B" -lt 300 ]; then
    warn "Collector B last data ${AGE_B}s ago"
else
    fail "Collector B no recent data (${AGE_B}s ago)"
fi

# Test 8: Device information
header "Test 8: Device Information"
DEVICE_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '.channels[0].device')
DEVICE_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '.channels[0].device')
FIPS_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '.channels[0].fips_code')
FIPS_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '.channels[0].fips_code')
DESIG_A=$(curl -s http://$COLLECTOR_A:8080/api/stats | jq -r '.channels[0].a_designation')
DESIG_B=$(curl -s http://$COLLECTOR_B:8080/api/stats | jq -r '.channels[0].a_designation')

echo "Collector A: $DEVICE_A ($FIPS_A-$DESIG_A)"
echo "Collector B: $DEVICE_B ($FIPS_B-$DESIG_B)"

pass "Device information retrieved"

# Test 9: Log files exist
header "Test 9: Log Files"
LOG_A=$(ssh root@$COLLECTOR_A "ls -lh /var/log/nectarcollector/$FIPS_A-$DESIG_A.log 2>&1 | awk '{print \$5}'")
LOG_B=$(ssh root@$COLLECTOR_B "ls -lh /var/log/nectarcollector/$FIPS_B-$DESIG_B.log 2>&1 | awk '{print \$5}'")

echo "Collector A log size: $LOG_A"
echo "Collector B log size: $LOG_B"

if [[ "$LOG_A" =~ ^[0-9]+[KMG]?$ ]]; then
    pass "Collector A log file exists"
else
    fail "Collector A log file not found"
fi

if [[ "$LOG_B" =~ ^[0-9]+[KMG]?$ ]]; then
    pass "Collector B log file exists"
else
    fail "Collector B log file not found"
fi

# Summary
header "Test Summary"
echo "Timestamp: $(date)"
echo ""
echo "All basic health checks complete."
echo "Review warnings and failures above."
echo ""
echo "For detailed testing, see TEST-PLAN.md"
echo "For live monitoring:"
echo "  Collector A: http://$COLLECTOR_A:8080/"
echo "  Collector B: http://$COLLECTOR_B:8080/"
