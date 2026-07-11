package main

import (
	"github.com/aeon022/habctl/cmd"
	"github.com/aeon022/habctl/internal/config"
)

func main() {
	// Apply saved API keys to env before any command runs.
	if cfg, err := config.Load(); err == nil {
		config.ApplyToEnv(cfg)
	}
	cmd.Execute()
}
