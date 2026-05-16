#!/bin/bash
# Build Canary Image on External Machine
# This script is for building zai-proxy canary images outside of devpod
# Use this when GitHub Actions is unavailable or for faster iteration

set -e

VERSION="${VERSION:-1.2.0-canary}"
IMAGE="${IMAGE:-ronaldraygun/zai-proxy:${VERSION}}"
DOCKER_REGISTRY="${DOCKER_REGISTRY:-docker.io}"

echo "========================================"
echo "ZAI Proxy Canary Build (External)"
echo "========================================"
echo "Version: $VERSION"
echo "Image: $IMAGE"
echo "Registry: $DOCKER_REGISTRY"
echo ""

# Check if we're in devpod (refuse to run there)
if [ -n "${DEVPOD:-}" ] || [ -f "/.devpod" ] || [ -d "/workspace" ]; then
    echo "ERROR: This script cannot run in devpod due to Docker overlayfs issues."
    echo ""
    echo "Please use one of these alternatives:"
    echo "  1. GitHub Actions: https://github.com/ardenone/ardenone-cluster/actions/workflows/build-canary-standard.yml"
    echo "  2. External machine: SSH to a server and run this script there"
    echo "  3. Local machine: Run this script on your local machine"
    exit 1
fi

# Check Docker availability
echo "Checking Docker..."
if ! command -v docker &> /dev/null; then
    echo "ERROR: Docker not found. Please install Docker first."
    echo ""
    echo "Install Docker: https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker info &> /dev/null; then
    echo "ERROR: Docker daemon not running. Please start Docker."
    exit 1
fi

echo "Docker is available."
echo ""

# Check if logged in to Docker Hub
echo "Checking Docker Hub authentication..."
if ! docker info | grep -q "Username"; then
    echo "Not logged in to Docker Hub. Please login:"
    echo "  docker login"
    echo ""
    docker login
fi

echo "Building image..."
echo ""

# Build the image
docker build \
    --build-arg "VERSION=${VERSION}" \
    --build-arg "GIT_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo 'unknown')" \
    --build-arg "BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -t "${IMAGE}" \
    -f Dockerfile .

echo ""
echo "Build complete!"
echo ""

# Ask if user wants to push
read -p "Push image to ${DOCKER_REGISTRY}? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Pushing image..."
    docker push "${IMAGE}"
    echo ""
    echo "Image pushed successfully!"
    echo ""
    echo "To deploy canary:"
    echo "  cd $(git rev-parse --show-toplevel)/containers/zai-proxy"
    echo "  ./tests/deploy-canary.sh"
else
    echo "Image not pushed. To push manually:"
    echo "  docker push ${IMAGE}"
fi

echo ""
echo "Done!"
