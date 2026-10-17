package main

import (
	"github.com/aeon022/habctl/cmd"
	"github.com/aeon022/habctl/internal/config"
	"github.com/aeon022/missionctl-core/secure"
)

func main() {
	secure.Init("habctl") // owner-only files from the first byte
	// Apply saved API keys to env before any command runs.
	if cfg, err := config.Load(); err == nil {
		config.ApplyToEnv(cfg, false)
	}
	cmd.Execute()
}
