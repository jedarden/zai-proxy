# Bead bf-26hx: Pluck Configuration Fix

## Summary

Fixed critical workspace path misconfiguration that was preventing Pluck from discovering beads in the zai-proxy workspace.

## Root Cause

The Needle configuration file at `~/.needle/config.yaml` had `workspace.default` set to `/home/coding/NEEDLE` instead of `/home/coding/zai-proxy`. This caused Pluck (running in "auto" mode) to look for beads in the wrong directory.

### Before Fix
```yaml
workspace:
  default: /home/coding/NEEDLE  # ❌ Wrong path
```

### After Fix
```yaml
workspace:
  default: /home/coding/zai-proxy  # ✅ Correct path
```

## Impact

- Pluck was unable to discover beads in the zai-proxy workspace
- This contributed to false starvation alerts when workers couldn't find work
- Previous investigation (bead bf-10or) incorrectly reported the configuration as correct

## Resolution

Updated the `workspace.default` path in `/home/coding/.needle/config.yaml` to point to the correct workspace.

## Verification

- ✅ Configuration value corrected
- ✅ Open beads confirmed accessible (29 open beads in workspace)
- ✅ Pluck should now correctly discover beads in `/home/coding/zai-proxy`

## Preventive Measures

To avoid future misconfigurations:
1. Verify workspace paths when setting up new projects
2. Cross-reference configuration claims against actual config files
3. Test bead discovery after any configuration changes

## Related Beads

- bf-1ugs: Starvation Alert Investigation (false positive caused by agent timeouts)
- bf-10or: Bead-worker Configuration Check (incorrect verification - missed the path issue)
- bf-kymf: Bead Database Verification (confirmed database integrity)
