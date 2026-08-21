#!/usr/bin/env bash
set -e

IMAGE_NAME="${1:?usage: $0 <image-name> [platform]}"
DOCKER_PLATFORM="${2:-linux/amd64}"

docker build --platform "$DOCKER_PLATFORM" -f eval.Dockerfile -t "$IMAGE_NAME" .
