# Bead Database Verification (bf-kymf)

## Database Integrity Check

**Command:** `br doctor`

**Result:** ✓ Database integrity: OK, ✓ JSONL validity: OK
- Database beads: 74
- JSONL beads: 74
- ⚠ Consistency: Drift detected (bf-kymf has hash mismatch)
- ⚠ Unflushed beads: 1

**Note:** The drift warning for bf-kymf is expected since this bead is currently open and being worked on.

## Open Beads Count

**Reported count:** 29 open beads
**Actual count:** 31 open beads

**Discrepancy:** 2 more open beads than reported (31 vs 29)

## Open Beads Sample

Sampled 5 open beads - all have complete and correct metadata:

1. **bf-5td** - Add unit tests for AdaptiveRateLimiter (AIMD/EWMA) in proxy
   - Status: open, Priority: P2, Type: task
   - Complete description, acceptance criteria, labels, dependencies

2. **bf-2io** - Add basic state and bounds tests for AdaptiveRateLimiter
   - Status: open, Priority: P2, Type: task
   - Complete description, acceptance criteria, labels, dependencies

3. **bf-ruw** - Add EWMA ceiling and convergence behavior tests for AdaptiveRateLimiter
   - Status: open, Priority: P2, Type: task
   - Complete description, acceptance criteria, labels, dependencies

4. **bf-63x** - Add probe, race safety, and env var tests for AdaptiveRateLimiter
   - Status: open, Priority: P2, Type: task
   - Complete description, acceptance criteria, labels, dependencies

5. **bf-5ga** - Add Reset() functionality tests
   - Status: open, Priority: P2, Type: task
   - Complete description, acceptance criteria, labels, dependencies

## Corruption Check

**Result:** No corrupted or malformed bead records found
- All sampled beads have proper metadata structure
- All required fields present (ID, Title, Status, Priority, Type, Description)
- Dependencies properly formatted
- No missing or truncated content

## Summary

- ✓ Database integrity verified
- ✓ JSONL validity confirmed
- ✗ Open bead count discrepancy (31 vs 29 reported)
- ✓ All sampled beads have correct metadata
- ✓ No corruption detected

**Recommendation:** The database is healthy. The count discrepancy may be due to:
1. Beads created since the last count report
2. Closed beads not yet flushed
3. Cached or stale count reference

Run `br sync --flush-only` to sync the JSONL checkpoint if needed.
