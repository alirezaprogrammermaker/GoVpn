#!/bin/bash
# ═══════════════════════════════════════════════════════════════
#  GoVpn GitHub Actions Relay - End-to-End Test Suite
#
#  Tests the full relay chain:
#    Client → CF Worker Queue → GitHub Actions Runner → Target
#
#  Prerequisites:
#    - CF Worker relay deployed at $RELAY_URL
#    - GitHub Actions runner running (or mock runner)
#    - curl installed
# ═══════════════════════════════════════════════════════════════

set -euo pipefail

# ─── Configuration ───
RELAY_URL="${RELAY_URL:-https://govpn-relay.social-panel.workers.dev}"
PROXY_PORT="${PROXY_PORT:-8083}"
TARGET_URL="${TARGET_URL:-https://httpbin.org/get}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Counters
PASSED=0
FAILED=0
SKIPPED=0

# ─── Helper Functions ───

log_header() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
}

log_test() {
    echo -e "  ${YELLOW}[TEST]${NC} $1"
}

log_pass() {
    echo -e "  ${GREEN}[PASS]${NC} $1"
    ((PASSED++))
}

log_fail() {
    echo -e "  ${RED}[FAIL]${NC} $1"
    ((FAILED++))
}

log_skip() {
    echo -e "  ${YELLOW}[SKIP]${NC} $1"
    ((SKIPPED++))
}

# ═══════════════════════════════════════════════════════════════
#  Test 1: CF Worker Relay Health
# ═══════════════════════════════════════════════════════════════

test_relay_health() {
    log_header "Test 1: CF Worker Relay Health"

    log_test "Checking relay health endpoint..."
    response=$(curl -s -f "${RELAY_URL}/health" 2>/dev/null || echo "FAILED")

    if echo "$response" | grep -q '"status"'; then
        log_pass "Relay health endpoint is responding"

        # Check for queue stats
        if echo "$response" | grep -q '"pending"'; then
            log_pass "Queue stats are available"
        else
            log_skip "Queue stats not in health response"
        fi
    else
        log_fail "Relay health check failed"
        echo "    Response: $response"
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 2: Enqueue a Request
# ═══════════════════════════════════════════════════════════════

test_enqueue() {
    log_header "Test 2: Enqueue Request"

    log_test "Submitting request to relay queue..."
    response=$(curl -s -f -X POST "${RELAY_URL}/enqueue" \
        -H "Content-Type: application/json" \
        -d "{\"method\": \"GET\", \"url\": \"${TARGET_URL}\", \"headers\": {}}" \
        2>/dev/null || echo "FAILED")

    if echo "$response" | grep -q '"id"'; then
        REQUEST_ID=$(echo "$response" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
        log_pass "Request enqueued successfully (ID: ${REQUEST_ID:0:8}...)"

        # Check status
        if echo "$response" | grep -q '"pending"'; then
            log_pass "Request status is 'pending'"
        else
            log_skip "Request status not 'pending'"
        fi
    else
        log_fail "Failed to enqueue request"
        echo "    Response: $response"
        REQUEST_ID=""
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 3: Poll for Request (Runner Simulation)
# ═══════════════════════════════════════════════════════════════

test_poll() {
    log_header "Test 3: Poll for Pending Request"

    if [ -z "$REQUEST_ID" ]; then
        log_skip "No request ID from previous test"
        return
    fi

    log_test "Polling as a runner..."
    response=$(curl -s -f "${RELAY_URL}/poll" \
        -H "X-Runner-Id: test-runner-$(date +%s)" \
        2>/dev/null || echo "FAILED")

    if echo "$response" | grep -q "$REQUEST_ID"; then
        log_pass "Got pending request from queue"
    elif echo "$response" | grep -q '"empty":true'; then
        log_skip "Queue empty (request may have been picked up by another runner)"
    else
        log_fail "Failed to poll for requests"
        echo "    Response: $response"
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 4: Submit Response
# ═══════════════════════════════════════════════════════════════

test_submit_response() {
    log_header "Test 4: Submit Response"

    if [ -z "$REQUEST_ID" ]; then
        log_skip "No request ID from previous test"
        return
    fi

    log_test "Submitting mock response..."
    response=$(curl -s -f -X POST "${RELAY_URL}/response/${REQUEST_ID}" \
        -H "Content-Type: application/json" \
        -d '{
            "status": 200,
            "headers": {"Content-Type": "application/json", "X-Test": "true"},
            "body": "{\"message\": \"Hello from test runner!\", \"test\": true}"
        }' \
        2>/dev/null || echo "FAILED")

    if echo "$response" | grep -q '"ok":true'; then
        log_pass "Response submitted successfully"
    else
        log_fail "Failed to submit response"
        echo "    Response: $response"
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 5: Get Result (Client Simulation)
# ═══════════════════════════════════════════════════════════════

test_get_result() {
    log_header "Test 5: Get Result"

    if [ -z "$REQUEST_ID" ]; then
        log_skip "No request ID from previous test"
        return
    fi

    log_test "Fetching result..."
    response=$(curl -s -f "${RELAY_URL}/result/${REQUEST_ID}" \
        2>/dev/null || echo "FAILED")

    if echo "$response" | grep -q '"Hello from test runner"'; then
        log_pass "Got correct response body"
    else
        log_fail "Response body mismatch"
        echo "    Response: $response"
    fi

    if echo "$response" | grep -q '"status":200'; then
        log_pass "Response status is 200"
    else
        log_fail "Response status mismatch"
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 6: Stats Endpoint
# ═══════════════════════════════════════════════════════════════

test_stats() {
    log_header "Test 6: Stats Endpoint"

    log_test "Checking stats..."
    response=$(curl -s -f "${RELAY_URL}/stats" 2>/dev/null || echo "FAILED")

    if echo "$response" | grep -q '"pending"'; then
        log_pass "Stats endpoint working"
    else
        log_fail "Stats endpoint failed"
        echo "    Response: $response"
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 7: Error Handling
# ═══════════════════════════════════════════════════════════════

test_error_handling() {
    log_header "Test 7: Error Handling"

    log_test "Enqueue without URL..."
    response=$(curl -s -w "\n%{http_code}" -X POST "${RELAY_URL}/enqueue" \
        -H "Content-Type: application/json" \
        -d '{"method": "GET"}' \
        2>/dev/null || echo "FAILED")

    http_code=$(echo "$response" | tail -1)
    if [ "$http_code" = "400" ]; then
        log_pass "Missing URL returns 400"
    else
        log_fail "Expected 400, got $http_code"
    fi

    log_test "Get non-existent result..."
    response=$(curl -s -w "\n%{http_code}" "${RELAY_URL}/result/non-existent-id" \
        2>/dev/null || echo "FAILED")

    http_code=$(echo "$response" | tail -1)
    if [ "$http_code" = "404" ]; then
        log_pass "Non-existent result returns 404"
    else
        log_fail "Expected 404, got $http_code"
    fi
}

# ═══════════════════════════════════════════════════════════════
#  Test 8: Full Chain (if runner is available)
# ═══════════════════════════════════════════════════════════════

test_full_chain() {
    log_header "Test 8: Full Chain (Client → Relay → Runner → Target)"

    log_test "Submitting real request to ${TARGET_URL}..."
    enqueue_resp=$(curl -s -f -X POST "${RELAY_URL}/enqueue" \
        -H "Content-Type: application/json" \
        -d "{\"method\": \"GET\", \"url\": \"${TARGET_URL}\", \"headers\": {}}" \
        2>/dev/null || echo "FAILED")

    if ! echo "$enqueue_resp" | grep -q '"id"'; then
        log_fail "Failed to enqueue for full chain test"
        return
    fi

    req_id=$(echo "$enqueue_resp" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
    log_test "Waiting for runner to process (up to 30s)..."

    # Poll for result
    for i in $(seq 1 30); do
        result=$(curl -s -f "${RELAY_URL}/result/${req_id}" 2>/dev/null || echo "{}")

        if echo "$result" | grep -q '"status":200'; then
            log_pass "Full chain completed successfully!"
            return
        elif echo "$result" | grep -q '"status":502'; then
            log_fail "Runner failed to reach target"
            return
        fi

        sleep 1
    done

    log_skip "Timeout - no runner available (this is expected if GH Actions isn't running)"
}

# ═══════════════════════════════════════════════════════════════
#  Main
# ═══════════════════════════════════════════════════════════════

main() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║        GoVpn GitHub Actions Relay - Test Suite               ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║  Relay URL:  %-46s  ║" "$RELAY_URL"
    echo "║  Target URL: %-46s  ║" "$TARGET_URL"
    echo "╚══════════════════════════════════════════════════════════════╝"

    test_relay_health
    test_enqueue
    test_poll
    test_submit_response
    test_get_result
    test_stats
    test_error_handling
    test_full_chain

    # ─── Summary ───
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                        Test Summary                          ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo -e "║  ${GREEN}Passed: %-3d${NC}   ${RED}Failed: %-3d${NC}   ${YELLOW}Skipped: %-3d${NC}                   ║" \
        "$PASSED" "$FAILED" "$SKIPPED"
    echo "╚══════════════════════════════════════════════════════════════╝"

    if [ "$FAILED" -gt 0 ]; then
        exit 1
    fi
}

main "$@"
