# Domain Fronting / SNI Rewriting Test Report

**Date:** 2026-07-30
**Target:** Google Apps Script Web App
**Script ID:** `112MhbriAuJgtY4Se_q8epebPlqxr9GqgH_BkoaRAU40vRA8Aay6tIIbR`

---

## Executive Summary

| Test | Result |
|------|--------|
| TLS Connection with front SNI | ✅ **WORKS** |
| HTTP Host header mismatch | ✅ **ACCEPTED** |
| Response (with redirect follow) | ✅ **WORKS** |
| Direct connection baseline | ✅ **WORKS** |

**Conclusion:** Domain Fronting is **POSSIBLE** with Google Apps Script infrastructure.

---

## How It Works

```
Client                          Google Infrastructure
  │                                    │
  │──── TLS ClientHello ──────────────►│
  │     SNI: www.google.com            │
  │     (front domain)                 │
  │                                    │
  │◄─── TLS ServerHello ──────────────│
  │     (connection established)       │
  │                                    │
  │──── HTTP Request ─────────────────►│
  │     Host: script.google.com        │
  │     (actual target)                │
  │                                    │
  │◄─── 302 Redirect ─────────────────│
  │     → script.googleusercontent.com │
  │     (normal Apps Script behavior)  │
  │                                    │
  │──── Follow Redirect ──────────────►│
  │     script.googleusercontent.com   │
  │                                    │
  │◄─── 200 OK ───────────────────────│
  │     {"message": "Hello..."}        │
```

---

## Technical Details

### TLS Layer
- **SNI sent:** Front domain (e.g., `www.google.com`)
- **Connection target:** Google IP (`142.251.142.110`)
- **Result:** TLS handshake succeeds ✅

### HTTP Layer
- **Host header:** `script.google.com` (actual target)
- **Google behavior:** Accepts mismatched Host header
- **Response:** 302 redirect to `script.googleusercontent.com`
- **Redirect follow:** Returns actual content ✅

---

## Tested Front Domains

| Front Domain | TLS Connect | HTTP Accept | Notes |
|-------------|-------------|-------------|-------|
| www.google.com | ✅ | ✅ | Best choice - common Google domain |
| googleapis.com | ✅ | ✅ | API domain - looks legitimate |
| www.googleapis.com | ✅ | ✅ | Same as above |
| fonts.googleapis.com | ✅ | ✅ | Font CDN - very common |
| storage.googleapis.com | ✅ | ✅ | Cloud storage |
| accounts.google.com | ✅ | ✅ | Auth domain |
| play.google.com | ✅ | ✅ | Play Store |
| mail.google.com | ✅ | ✅ | Gmail |
| docs.google.com | ✅ | ✅ | Google Docs |
| drive.google.com | ✅ | ✅ | Google Drive |

---

## Implications for VPN

### Advantages
1. **Traffic appears legitimate** - SNI shows common Google domains
2. **Hard to block** - Blocking Google domains affects all Google services
3. **No special server needed** - Uses existing Google infrastructure
4. **Encrypted** - TLS protects the actual request

### Limitations
1. **302 redirect** - Final request goes to `script.googleusercontent.com`
2. **Redirect URL visible** - Deep packet inspection could detect the redirect
3. **Google could block** - Google may disable this in the future
4. **Rate limiting** - Google may rate limit suspicious patterns

---

## Recommendations

For VPN implementation:
1. Use `www.google.com` or `fonts.googleapis.com` as front domains
2. Follow redirects programmatically
3. Rotate front domains to avoid detection
4. Consider combining with other obfuscation techniques

---

## Test Files

- `test.sh` - Bash test script
- `main.go` - Go test program
- `REPORT.md` - This report

---

**Status:** ✅ Domain Fronting confirmed working with Google Apps Script
