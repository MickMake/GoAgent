package main

import (
	"github.com/MickMake/GoAgent/providers/fortune"
	"github.com/MickMake/GoAgent/providers/shell"
)

func shellRuntime(cfg AppConfig) shell.Runtime {
	cfg = normalizeConfig(cfg)
	return shell.Runtime{
		CommandTimeoutSeconds: cfg.Runtime.CommandTimeoutSeconds,
		OutputLimitBytes:      cfg.Runtime.OutputLimitBytes,
		ChildEnv:              append([]string(nil), cfg.Runtime.ChildEnv...),
	}
}

func fortuneRuntime(cfg AppConfig) fortune.Runtime {
	cfg = normalizeConfig(cfg)
	return fortune.Runtime{
		CommandTimeoutSeconds: cfg.Runtime.CommandTimeoutSeconds,
		OutputLimitBytes:      cfg.Runtime.OutputLimitBytes,
		ChildEnv:              append([]string(nil), cfg.Runtime.ChildEnv...),
	}
}
