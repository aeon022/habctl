#!/bin/bash
set -e
cd "$(dirname "$0")"
go build -o habctl .
mkdir -p ~/.local/bin
mv habctl ~/.local/bin/habctl
echo "✓ habctl installed to ~/.local/bin/habctl — run 'habctl add \"Sport\"' to start"

# ── MCP: register in ~/.claude.json ───────────────────────────────────────────
CLAUDE_JSON="$HOME/.claude.json"
if command -v python3 &>/dev/null; then
  python3 - "$CLAUDE_JSON" "$HOME/.local/bin/habctl" <<'PYEOF'
import json, sys, os

claude_json = sys.argv[1]
binary_path = sys.argv[2]

data = {}
if os.path.exists(claude_json):
    with open(claude_json) as f:
        try:
            data = json.load(f)
        except Exception:
            pass

data.setdefault("mcpServers", {})
data["mcpServers"]["habctl"] = {
    "command": binary_path,
    "args": ["mcp"]
}

with open(claude_json, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("✓ MCP server registered in ~/.claude.json")
print("  Restart Claude Code to activate habctl MCP tools")
PYEOF
else
  echo "  To enable MCP (Claude Code integration), add to ~/.claude.json:"
  echo "  \"mcpServers\": { \"habctl\": { \"command\": \"$HOME/.local/bin/habctl\", \"args\": [\"mcp\"] } }"
fi
