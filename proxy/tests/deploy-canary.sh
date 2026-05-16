#!/bin/bash
# Deploy Canary Deployment Script for ZAI Proxy v1.2.0
# This script deploys the canary version with token counting enabled

set -e

NAMESPACE="${NAMESPACE:-devpod}"
IMAGE="${IMAGE:-ronaldraygun/zai-proxy:1.2.0-canary}"

echo "========================================"
echo "ZAI Proxy Canary Deployment"
echo "========================================"
echo "Namespace: $NAMESPACE"
echo "Image: $IMAGE"
echo ""

# Pre-flight checks
echo "Pre-flight checks..."

# Check if production is stable
echo -n "Checking production pods... "
PROD_PODS=$(kubectl get pods -n "$NAMESPACE" -l app=zai-proxy --no-headers 2>/dev/null | grep Running | wc -l || echo "0")
if [ "$PROD_PODS" -ge 5 ]; then
    echo "OK ($PROD_PODS running)"
else
    echo "WARNING: Only $PROD_PODS production pods running"
fi

# Check if canary already exists
echo -n "Checking existing canary... "
if kubectl get deployment -n "$NAMESPACE" zai-proxy-canary >/dev/null 2>&1; then
    echo "EXISTS - will upgrade"
    CANARY_EXISTS=true
else
    echo "NOT FOUND - will create new"
    CANARY_EXISTS=false
fi

# Apply manifests
echo ""
echo "Applying Kubernetes manifests..."

# Apply deployment
echo "Applying deployment..."
sed "s|image: ronaldraygun/zai-proxy:1.2.0-canary|image: $IMAGE|g" \
    k8s/canary/deployment.yml | kubectl apply -n "$NAMESPACE" -f -

# Apply service
if [ "$CANARY_EXISTS" = "false" ]; then
    echo "Applying service..."
    kubectl apply -n "$NAMESPACE" -f k8s/canary/service.yml
else
    echo "Service already exists, skipping..."
fi

# Wait for rollout
echo ""
echo "Waiting for canary rollout..."
kubectl rollout status deployment/zai-proxy-canary -n "$NAMESPACE" --timeout=2m

# Verify deployment
echo ""
echo "Verifying deployment..."
CANARY_PODS=$(kubectl get pods -n "$NAMESPACE" -l app=zai-proxy-canary --no-headers 2>/dev/null | grep Running | wc -l || echo "0")
if [ "$CANARY_PODS" -ge 1 ]; then
    echo "✓ Canary deployment successful: $CANARY_PODS pod(s) running"
else
    echo "✗ Canary deployment failed"
    exit 1
fi

# Get canary pod name for logs
CANARY_POD=$(kubectl get pods -n "$NAMESPACE" -l app=zai-proxy-canary -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -n "$CANARY_POD" ]; then
    echo ""
    echo "Canary pod: $CANARY_POD"
    echo ""
    echo "Recent logs:"
    kubectl logs -n "$NAMESPACE" "$CANARY_POD" --tail=10 2>/dev/null || echo "Unable to fetch logs"
fi

# Check for token counting in logs
if [ -n "$CANARY_POD" ]; then
    echo ""
    if kubectl logs -n "$NAMESPACE" "$CANARY_POD" 2>/dev/null | grep -q "Token counting enabled"; then
        echo "✓ Token counting is enabled"
    else
        echo "⚠ Token counting may not be enabled (check logs above)"
    fi
fi

echo ""
echo "========================================"
echo "Canary Deployment Complete"
echo "========================================"
echo ""
echo "Service endpoint:"
echo "  ClusterIP: zai-proxy-canary.$NAMESPACE.svc.cluster.local:8080"
echo ""
echo "Next steps:"
echo "  1. Run tests: ./tests/run-canary-tests.sh"
echo "  2. Monitor logs: kubectl logs -n $NAMESPACE -l app=zai-proxy-canary -f"
echo "  3. Check metrics: kubectl port-forward -n $NAMESPACE svc/zai-proxy-canary 8080:8080"
echo "     Then open: http://localhost:8080/metrics"
echo ""
echo "To roll back:"
echo "  kubectl delete deployment zai-proxy-canary -n $NAMESPACE"
echo "  kubectl delete service zai-proxy-canary -n $NAMESPACE"
echo ""
