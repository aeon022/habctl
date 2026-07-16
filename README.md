# habctl

Terminal-first habit tracker. Part of the **missionctl** suite.

Track daily habits, build streaks, get AI-powered suggestions and weekly coaching reviews — all from your terminal, stored locally in SQLite.

---

## Quick Start

```bash
# Build & install
./setup.sh

# Add your first habits
habctl add "Meditate" --desc "10 minutes every morning"
habctl add "Read"

# Check in for today
habctl check Meditate

# See where you stand
habctl today
habctl stats

# Open the interactive TUI
habctl
```

### MCP configuration (Claude Desktop / Claude Code)

```json
{
  "mcpServers": {
    "habctl": {
      "command": "habctl",
      "args": ["mcp"]
    }
  }
}
```

---

## Cheatsheet

### CLI

| Command | Description |
|---|---|
| `habctl` | Open TUI |
| `habctl add NAME [--desc TEXT]` | Add a new habit |
| `habctl check NAME [--date YYYY-MM-DD]` | Check in a habit for today (or a given date) |
| `habctl today` | Today's habit status at a glance |
| `habctl list` | List all habits with today's status |
| `habctl stats` | Statistics with streaks and progress bars |
| `habctl review` | AI weekly review — coaching briefing for the last 7 days |
| `habctl suggest [--goal TEXT] [--routine TEXT]` | AI-powered habit suggestions |
| `habctl remind` | macOS notification for unchecked habits |
| `habctl delete NAME` | Delete a habit and all its check-ins |
| `habctl mcp` | Start MCP server (stdio) |

### TUI keys

| Key | Action |
|---|---|
| `j` / `k`, `↑` / `↓` | Move selection |
| `space` | Check in / undo check-in (toggle) |
| `enter` | Open habit (detail, description, note history) |
| `n` | New habit (optional emoji prefix) |
| `N` | Add note to today's check-in |
| `e` | Edit habit (name, desc, frequency, skip) |
| `a` / `A` | Archive habit / open archive |
| `d` | Delete habit permanently |
| `m` / `G` | Move habit to group / manage groups |
| `s` | AI suggestions (context-aware) |
| `g` | Goal → 3 linked habits (decompose) |
| `r` | AI weekly review — coaching briefing |
| `t` | Stats — heatmap & completion |
| `c` | Manage habit chains |
| `S` | Settings — provider & API keys |
| `v` / `w` | Compact toggle / 7d–30d streak window |
| `?` | Help |
| `q` / `ctrl+c` | Quit |

---

## AI Integration

`suggest` and `review` use an LLM. Configure a provider once — environment variables always win over the config file (`~/.config/habctl/config.json`):

| Provider | Setup |
|---|---|
| Anthropic (Claude) | `export ANTHROPIC_API_KEY=sk-ant-...` |
| OpenAI | `export OPENAI_API_KEY=sk-...` |
| Google Gemini | API key **or** browser OAuth login (Google Cloud Desktop-app client) |
| Ollama (local) | `ollama_host` + `ollama_model` in config, no key needed |

```bash
habctl suggest --goal "more focus, less screen time" --count 6
habctl review          # coaching briefing for the last 7 days
```

## MCP Tools

12 tools for AI agents: `list_habits`, `check_habit`, `uncheck_habit`, `add_habit`, `delete_habit`, `get_habit_stats`, `streak_at_risk`, `get_weekly_summary`, `get_weekly_review`, `suggest_habits`, `add_checkin_note`, `list_chains`.

Example prompts for Claude:

> "Which of my streaks are at risk today?"
> "Check in meditation and add a note that it was hard to focus."
> "Give me a weekly review of my habits."

---

## Data

Everything lives in a local SQLite database at `~/.local/share/habctl/habits.db`. No cloud, no account, no telemetry. AI provider settings are stored in `~/.config/habctl/config.json`.

## Part of missionctl

habctl is one tool in the [missionctl](https://github.com/aeon022/missionctl) suite — local-first terminal tools that give AI hands: mailctl, calctl, taskctl, notectl, budgetctl, habctl, timectl, diaryctl, postctl.
