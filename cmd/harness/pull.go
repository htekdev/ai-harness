package main

import (
	"flag"
	"fmt"

	"github.com/htekdev/ai-harness/config"
)

func cmdPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgPath := resolveConfig(*configPath)
	cfg, err := config.LoadAuto(cfgPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", cfgPath, err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	if len(cfg.ArtifactSources) == 0 {
		fmt.Println("No artifact_sources configured.")
		return nil
	}

	baseDir := dirOfConfig(cfgPath)
	if _, err := config.LoadDirectoryWithSources(baseDir, cfg.ArtifactSources, cfg.TrustedSources, true); err != nil {
		return fmt.Errorf("pull artifact sources: %w", err)
	}

	fmt.Printf("Fetched %d artifact source(s).\n", len(cfg.ArtifactSources))
	return nil
}
