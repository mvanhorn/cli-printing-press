#!/usr/bin/env bash
# Render all four launch video outputs.
#
# Run from launch-video/ directory.
# Requires: Node 20+, dependencies installed (npm install), and Chrome Canary
# (handled by --gl=angle which routes through Remotion's bundled Canary build
# on v4.0.455+).
#
# Outputs land in out/ and are gitignored. See README.md ship checklist before
# treating any output as final.

set -euo pipefail

cd "$(dirname "$0")/.."

mkdir -p out

echo "==> Rendering hero (1920x1080, 45s)"
npx remotion render src/Root.tsx Hero out/hero-45s.mp4 \
  --gl=angle \
  --concurrency=2

echo "==> Rendering hero vertical (1080x1920, 45s)"
npx remotion render src/Root.tsx HeroVertical out/hero-vertical-45s.mp4 \
  --gl=angle \
  --concurrency=2

echo "==> Rendering hero square (1080x1080, 45s)"
npx remotion render src/Root.tsx HeroSquare out/hero-square-45s.mp4 \
  --gl=angle \
  --concurrency=2

echo "==> Rendering 15s cutdown (frames 270-720 of hero)"
npx remotion render src/Root.tsx Hero out/cutdown-15s.mp4 \
  --gl=angle \
  --frames=270-720 \
  --concurrency=2

echo
echo "==> All renders complete. Outputs:"
ls -la out/*.mp4
echo
echo "==> Run the ship checklist in README.md before publishing."
