package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

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

// Regression test: the CLI's "Add with:" hint used to be one generic,
// content-losing line ("Add with: habctl add \"<name>\"") regardless of
// what the AI actually suggested — the Time/Benefit/Tip fields it just
// streamed and printed above were discarded the moment it told you how to
// save one. printAddHints now parses its own response and prints a
// ready-to-run command per suggestion, carrying that content over via
// --desc.
func TestPrintAddHints_CarriesContentIntoDescFlag(t *testing.T) {
	muted := lipgloss.NewStyle()
	lime := lipgloss.NewStyle()
	raw := "###\nName: Daily Core Workout\nTime: 15 min/day\nBenefit: Builds core strength.\nTip: Start with planks.\n###"

	out := captureStdout(t, func() { printAddHints(raw, muted, lime) })

	if !strings.Contains(out, `habctl add "Daily Core Workout"`) {
		t.Errorf("output missing the add command for the suggestion:\n%s", out)
	}
	if !strings.Contains(out, "--desc") || !strings.Contains(out, "Builds core strength.") {
		t.Errorf("output missing --desc with the suggestion's content:\n%s", out)
	}
	if strings.Contains(out, `"<name>"`) {
		t.Errorf("output still contains the old generic placeholder hint:\n%s", out)
	}
}

// A quote inside a suggested name/benefit must not break the printed
// command if copy-pasted into a shell.
func TestPrintAddHints_EscapesQuotesInContent(t *testing.T) {
	muted := lipgloss.NewStyle()
	lime := lipgloss.NewStyle()
	raw := `###
Name: The "Deep Work" Block
Benefit: Say "no" to distractions.
###`

	out := captureStdout(t, func() { printAddHints(raw, muted, lime) })

	if !strings.Contains(out, `\"Deep Work\"`) {
		t.Errorf("output doesn't escape the quote in the name:\n%s", out)
	}
	if !strings.Contains(out, `\"no\"`) {
		t.Errorf("output doesn't escape the quote in the description:\n%s", out)
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
