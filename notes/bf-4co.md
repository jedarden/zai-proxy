# Bead bf-4co: README.md Fixes Already Completed

This bead's requirements were already satisfied by commit `a106abd` on 2026-07-02:

```
commit a106abd09d21db8f2bf728f2f5d6d6762faed512
Author: jedarden <me@jedcabanero.com>
Date:   Thu Jul 2 07:47:43 2026 -0400

    docs(readme): fix metric names, dashboard port, and env var defaults
    
    - Fix metric names to use zai_proxy_ prefix (matching proxy/metrics.go)
    - Fix dashboard port from :3000 to :8080 (matching DefaultConfig)
    - Fix env var defaults: RATE_LIMIT_INITIAL=10.0, RATE_LIMIT_MIN=1.0, RATE_LIMIT_MAX=50.0, MAX_RETRIES=3
    - Add missing RATE_LIMIT_* tunables: CEILING_ALPHA=0.3, HOLD_MARGIN=0.02, PROBE_INTERVAL=10
    - Fix ZAI_TARGET_URL default to https://api.z.ai/api/anthropic
    - Update description to reflect Z.AI-specific implementation (not OpenAI-compatible)
```

## Verification

All acceptance criteria met:

- ✓ Every metric name in README.md matches a Name: field in proxy/metrics.go
- ✓ Ports and env defaults in README.md match proxy/main.go and dashboard/api/router.go  
- ✓ The three RATE_LIMIT_* tunables (CEILING_ALPHA, HOLD_MARGIN, PROBE_INTERVAL) appear in the env table with correct defaults
- ✓ No claim that the proxy is generic/OpenAI-compatible
- ✓ Doc-only change; no code changes

No additional work required.
