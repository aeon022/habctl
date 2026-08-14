package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/charmbracelet/lipgloss"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything it printed — printAddHints writes via fmt.Println directly
// rather than taking a Writer, so this is the simplest way to assert on it
// without changing that signature just for testability.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	return buf.String()
}

// Regression test: the CLI's "Add with:" hint went through two shapes
// before this one. Originally one generic, content-losing line ("Add with:
// habctl add \"<name>\"") regardless of what the AI actually suggested —
// the Time/Benefit/Tip fields it just streamed and printed above were
// discarded the moment it told you how to save one. Fixed to print the full
// name and --desc inline, which fixed the content loss but traded it for a
// new problem: an emoji-prefixed name is awkward to type or copy-paste
// correctly in a terminal. printAddHints now caches the parsed suggestions
// (SaveLastSuggestions) and prints just "habctl add <N>" per one — nothing
// left to type or copy at all, see resolveAddArgs in cmd/add.go for the
// lookup this enables.
func TestPrintAddHints_PrintsNumberedCommandsAndCachesForAdd(t *testing.T) {
	t.Cleanup(func() { ai.SaveLastSuggestions(nil) })

	muted := lipgloss.NewStyle()
	lime := lipgloss.NewStyle()
	raw := "###\nName: Daily Core Workout\nTime: 15 min/day\nBenefit: Builds core strength.\nTip: Start with planks.\n###\n###\nName: Evening Reading\nBenefit: Winds down the day.\n###"

	out := captureStdout(t, func() { printAddHints(raw, muted, lime) })

	if !strings.Contains(out, "habctl add 1") || !strings.Contains(out, "habctl add 2") {
		t.Errorf("output missing numbered add commands for both suggestions:\n%s", out)
	}
	if !strings.Contains(out, "Daily Core Workout") || !strings.Contains(out, "Evening Reading") {
		t.Errorf("output missing the suggestion names for reference:\n%s", out)
	}
	if strings.Contains(out, `"<name>"`) {
		t.Errorf("output still contains the old generic placeholder hint:\n%s", out)
	}

	cached, err := ai.LoadLastSuggestions()
	if err != nil {
		t.Fatalf("LoadLastSuggestions() error = %v", err)
	}
	if len(cached) != 2 || cached[0].Name != "Daily Core Workout" || cached[1].Name != "Evening Reading" {
		t.Errorf("cached suggestions = %+v, want both parsed suggestions in order", cached)
	}
}

// A response that doesn't parse into any suggestions (model ignored the
// format) must fall back to the old generic hint rather than printing
// nothing at all.
func TestPrintAddHints_FallsBackWhenNothingParses(t *testing.T) {
	muted := lipgloss.NewStyle()
	lime := lipgloss.NewStyle()

	out := captureStdout(t, func() { printAddHints("some free-form text with no ### blocks", muted, lime) })

	if !strings.Contains(out, `Add with: habctl add "<name>"`) {
		t.Errorf("expected fallback generic hint when nothing parses, got:\n%s", out)
	}
}
