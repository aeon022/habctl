#!/bin/bash
set -e
cd "$(dirname "$0")"
go build -o habctl .
sudo mv habctl /usr/local/bin/habctl
echo "habctl installed — run 'habctl add \"Sport\"' to start"
