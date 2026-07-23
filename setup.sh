#!/bin/bash
set -e
cd "$(dirname "$0")"
go build -o habctl .
mkdir -p ~/.local/bin
mv habctl ~/.local/bin/habctl
echo "✓ habctl installed to ~/.local/bin/habctl — run 'habctl add \"Sport\"' to start"
