package main

import (
	"github.com/aeon022/habctl/cmd"
	"github.com/aeon022/habctl/internal/config"
)

func main() {
	// Load saved config and apply to env before any command runs.
	if cfg, err := config.Load(); err == nil {
		config.ApplyToEnv(cfg)
	}
	cmd.Execute()
}
