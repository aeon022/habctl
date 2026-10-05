package cmd

import (
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
)

// out is stdout for styled CLI output: lipgloss v2 always emits ANSI, so each
// write goes through a colorprofile.Writer that downsamples or strips it for
// pipes, NO_COLOR and dumb terminals. It looks up os.Stdout on every write
// (not once at init) so tests that swap os.Stdout still capture the output.
var out io.Writer = stdout{}

type stdout struct{}

func (stdout) Write(p []byte) (int, error) {
	return colorprofile.NewWriter(os.Stdout, os.Environ()).Write(p)
}
