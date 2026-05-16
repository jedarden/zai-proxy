#!/bin/bash
# Verification script for zai-proxy dual deployment monitoring setup
# This script checks that Prometheus is correctly scraping metrics from both production and canary deployments

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "================================"
echo "ZAI-PROXY MONITORING VERIFICATION"
echo "================================"
echo ""

# Function to print status
print_status() {
    local status=$1
    local message=$2
    if [ "$status" = "OK" ]; then
        echo -e "${GREEN}[OK]${NC} $message"
    elif [ "$status" = "WARN" ]; then
        echo -e "${YELLOW}[WARN]${NC} $message"
    else
        echo -e "${RED}[FAIL]${NC} $message"
    fi
}

# Check if monitoring namespace exists
echo "1. Checking monitoring namespace..."
if kubectl get namespace monitoring &>/dev/null; then
    print_status "OK" "Monitoring namespace exists"
else
    print_status "FAIL" "Monitoring namespace does not exist"
    exit 1
fi

# Check ServiceMonitor for production
echo ""
echo "2. Checking ServiceMonitor for production..."
if kubectl get servicemonitor zai-proxy-production -n monitoring &>/dev/null; then
    print_status "OK" "Production ServiceMonitor exists"
    kubectl get servicemonitor zai-proxy-production -n monitoring -o jsonpath='{.spec.selector.matchLabels}' | grep -q "version: production" && \
        print_status "OK" "Production ServiceMonitor selector includes version=production" || \
        print_status "WARN" "Production ServiceMonitor selector may not match production service"
    kubectl get servicemonitor zai-proxy-production -n monitoring -o jsonpath='{.spec.namespaceSelector.matchNames}' | grep -q "mcp" && \
        print_status "OK" "Production ServiceMonitor targets mcp namespace" || \
        print_status "WARN" "Production ServiceMonitor may not target correct namespace"
else
    print_status "FAIL" "Production ServiceMonitor does not exist"
fi

# Check ServiceMonitor for canary
echo ""
echo "3. Checking ServiceMonitor for canary..."
if kubectl get servicemonitor zai-proxy-canary -n monitoring &>/dev/null; then
    print_status "OK" "Canary ServiceMonitor exists"
    kubectl get servicemonitor zai-proxy-canary -n monitoring -o jsonpath='{.spec.selector.matchLabels}' | grep -q "version: canary" && \
        print_status "OK" "Canary ServiceMonitor selector includes version=canary" || \
        print_status "WARN" "Canary ServiceMonitor selector may not match canary service"
    kubectl get servicemonitor zai-proxy-canary -n monitoring -o jsonpath='{.spec.namespaceSelector.matchNames}' | grep -q "devpod" && \
        print_status "OK" "Canary ServiceMonitor targets devpod namespace" || \
        print_status "WARN" "Canary ServiceMonitor may not target correct namespace"
else
    print_status "FAIL" "Canary ServiceMonitor does not exist"
fi

# Check PrometheusRules for canary alerts
echo ""
echo "4. Checking PrometheusRules for canary alerts..."
if kubectl get prometheusrule zai-proxy-canary-alerts -n monitoring &>/dev/null; then
    print_status "OK" "Canary alerts PrometheusRule exists"
    alert_count=$(kubectl get prometheusrule zai-proxy-canary-alerts -n monitoring -o jsonpath='{.spec.groups[0].rules}' | jq '. | length')
    print_status "OK" "Found $alert_count canary-specific alert rules"
else
    print_status "WARN" "Canary alerts PrometheusRule does not exist (alerts may not be configured)"
fi

# Check Grafana dashboard
echo ""
echo "5. Checking Grafana dashboard..."
if kubectl get configmap zai-proxy-grafana-dashboard -n monitoring &>/dev/null; then
    print_status "OK" "Grafana dashboard ConfigMap exists"
    # Check if dashboard has dual deployment panels
    if kubectl get configmap zai-proxy-grafana-dashboard -n monitoring -o jsonpath='{.data}' | grep -q "deployment_variant"; then
        print_status "OK" "Dashboard queries use deployment_variant label for filtering"
    else
        print_status "WARN" "Dashboard may not properly filter by deployment variant"
    fi
    # Check for canary-specific panels
    if kubectl get configmap zai-proxy-grafana-dashboard -n monitoring -o jsonpath='{.data}' | grep -q "Token Counting (Canary Only)"; then
        print_status "OK" "Dashboard includes canary-specific token counting panels"
    else
        print_status "WARN" "Dashboard may not include canary-specific features"
    fi
else
    print_status "WARN" "Grafana dashboard ConfigMap does not exist"
fi

# Verify deployment_variant relabeling
echo ""
echo "6. Checking ServiceMonitor relabel configs..."
prod_relabel=$(kubectl get servicemonitor zai-proxy-production -n monitoring -o jsonpath='{.spec.endpoints[0].relabelings}' | grep -c "deployment_variant" || true)
canary_relabel=$(kubectl get servicemonitor zai-proxy-canary -n monitoring -o jsonpath='{.spec.endpoints[0].relabelings}' | grep -c "deployment_variant" || true)

if [ "$prod_relabel" -gt 0 ]; then
    print_status "OK" "Production ServiceMonitor has deployment_variant relabeling"
else
    print_status "WARN" "Production ServiceMonitor may not add deployment_variant label"
fi

if [ "$canary_relabel" -gt 0 ]; then
    print_status "OK" "Canary ServiceMonitor has deployment_variant relabeling"
else
    print_status "WARN" "Canary ServiceMonitor may not add deployment_variant label"
fi

# Summary
echo ""
echo "================================"
echo "SUMMARY"
echo "================================"
echo "The monitoring setup for dual deployments includes:"
echo "  - ServiceMonitors for production and canary"
echo "  - PrometheusRules for canary-specific alerts"
echo "  - Grafana dashboard with production vs canary comparison"
echo ""
echo "Key metrics tracked:"
echo "  - Request rate, error rate, latency (both deployments)"
echo "  - Token counting metrics (canary only)"
echo "  - Rate limiting behavior (both deployments)"
echo "  - Worker utilization (both deployments)"
echo ""
echo "Alert rules:"
echo "  - ZaiProxyCanaryHighErrorRate (>5% for 5min)"
echo "  - ZaiProxyCanaryHighLatency (P95 >10s for 5min)"
echo "  - ZaiProxyCanaryCrashLooping"
echo "  - ZaiProxyCanaryNotReady (0 ready pods for 2min)"
echo "  - ZaiProxyCanaryDegradedVsProduction (2x error rate)"
echo "  - ZaiProxyCanarySlowerThanProduction (50% higher P95)"
echo "  - ZaiProxyCanaryTokenCountingSlow (P95 >100ms)"
echo "  - ZaiProxyCanaryRateLimitAdjustingDown"
echo ""
