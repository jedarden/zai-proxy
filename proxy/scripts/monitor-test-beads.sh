#!/bin/bash
# Monitor test beads created for workspace discovery experiment (bd-3uv)

set -euo pipefail

echo "=== Workspace Discovery Test Monitoring ==="
echo "Monitoring test beads: bd-34zc, bd-7cp, bd-133"
echo ""

# Function to check a bead in a specific workspace
check_bead() {
    local workspace=$1
    local bead_id=$2
    local workspace_name=$(basename "$workspace")

    echo "--- $workspace_name (${bead_id}) ---"
    if cd "$workspace" 2>/dev/null; then
        if br show "$bead_id" 2>/dev/null | grep -E "(Status|Assignee|Updated|Labels)"; then
            echo ""
        else
            echo "ERROR: Could not retrieve bead $bead_id"
            echo ""
        fi
    else
        echo "ERROR: Workspace not found: $workspace"
        echo ""
    fi
}

# Check each test bead
check_bead "/home/coder/botburrow-hub" "bd-34zc"
check_bead "/home/coder/ringmaster" "bd-7cp"
check_bead "/home/coder/ardenone-cluster/containers/zai-proxy" "bd-133"

# Check for any workers
echo "=== Active Workers ==="
if command -v tlist &> /dev/null; then
    tlist | grep -i worker || echo "No workers found"
else
    echo "tlist command not available"
fi
echo ""

# Summary
echo "=== Summary ==="
echo "Run this script periodically to monitor worker discovery"
echo "Expected: Workers should discover and offer to work on these beads"
echo ""
echo "Manual cleanup when done:"
echo "  cd /home/coder/botburrow-hub && br close bd-34zc --reason 'Discovery test complete'"
echo "  cd /home/coder/ringmaster && br close bd-7cp --reason 'Discovery test complete'"
echo "  br close bd-133 --reason 'Discovery test complete'"
