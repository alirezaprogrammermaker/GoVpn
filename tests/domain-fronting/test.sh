#!/bin/bash
# ============================================================
# Domain Fronting / SNI Rewriting Test
# Tests Google Apps Script endpoint for domain fronting support
# ============================================================

TARGET_HOST="script.google.com"
TARGET_PATH="/macros/s/AKfycbzEvNqf9p_4EuY7KGdXN3BAJLUfMJp6jsR5SQHFJfhI6nCter-8EDHSs-OjCP4DHXJ3/exec"
FRONT_DOMAINS=("www.google.com" "googleapis.com" "www.googleapis.com" "fonts.googleapis.com" "storage.googleapis.com")

echo "============================================"
echo " Domain Fronting / SNI Rewriting Test"
echo "============================================"
echo ""
echo "Target: https://${TARGET_HOST}${TARGET_PATH}"
echo ""

# Resolve target IP first
echo "[*] Resolving ${TARGET_HOST}..."
TARGET_IP=$(nslookup ${TARGET_HOST} 2>/dev/null | grep "Address:" | tail -1 | awk '{print $2}')
echo "[*] Target IP: ${TARGET_IP}"
echo ""

# Test 1: Direct connection (baseline)
echo "============================================"
echo " TEST 1: Direct Connection (Baseline)"
echo "============================================"
RESULT=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "https://${TARGET_HOST}${TARGET_PATH}" 2>&1)
echo "  Status Code: ${RESULT}"
if [ "$RESULT" = "200" ]; then
    echo "  ✅ Direct connection works"
else
    echo "  ❌ Direct connection failed"
fi
echo ""

# Test 2: Domain Fronting with Host header
echo "============================================"
echo " TEST 2: Domain Fronting (Host Header)"
echo "============================================"
for FRONT in "${FRONT_DOMAINS[@]}"; do
    echo "  Testing front: ${FRONT}"
    RESULT=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 \
        --resolve "${TARGET_HOST}:443:${TARGET_IP}" \
        -H "Host: ${TARGET_HOST}" \
        "https://${FRONT}${TARGET_PATH}" 2>&1)
    if [ "$RESULT" = "200" ]; then
        echo "    Status: ${RESULT} ✅ FRONTING WORKS"
    elif [ "$RESULT" = "404" ] || [ "$RESULT" = "421" ]; then
        echo "    Status: ${RESULT} ⚠️  Fronting blocked (misdirected)"
    else
        echo "    Status: ${RESULT} ❌ Failed"
    fi
done
echo ""

# Test 3: SNI Rewriting (different SNI vs Host)
echo "============================================"
echo " TEST 3: SNI Rewriting"
echo "============================================"
for FRONT in "${FRONT_DOMAINS[@]}"; do
    echo "  SNI: ${FRONT} → Host: ${TARGET_HOST}"
    RESULT=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 \
        --resolve "${TARGET_HOST}:443:${TARGET_IP}" \
        --resolve "${FRONT}:443:${TARGET_IP}" \
        -H "Host: ${TARGET_HOST}" \
        "https://${FRONT}${TARGET_PATH}" 2>&1)
    if [ "$RESULT" = "200" ]; then
        echo "    Status: ${RESULT} ✅ SNI REWRITE WORKS"
    else
        echo "    Status: ${RESULT} ❌ SNI rewrite failed"
    fi
done
echo ""

# Test 4: cURL with explicit SNI override
echo "============================================"
echo " TEST 4: Explicit SNI Override (--resolve)"
echo "============================================"
echo "  SNI: www.google.com → resolving to ${TARGET_IP}"
BODY=$(curl -s --max-time 10 \
    --resolve "www.google.com:443:${TARGET_IP}" \
    -H "Host: ${TARGET_HOST}" \
    "https://www.google.com${TARGET_PATH}" 2>&1)
echo "  Response: ${BODY}" | head -3
echo ""

echo "============================================"
echo " Test Complete"
echo "============================================"
