package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: harness init [name]

Scaffold a new harness project by copying from the core harness.

Arguments:
  name    Project name (default: current directory name)

Creates:
  harness.md              Main harness configuration
  .harness/tools/         Tool definitions (including default file tools)
  .harness/hooks/         Hook definitions (including safety hooks)
  .harness/agents/        Agent definitions

`)
	}
	fs.Parse(args)

	name := fs.Arg(0)
	if name == "" {
		// Use current directory name
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		name = filepath.Base(cwd)
	}

	// Check if harness.md already exists
	if _, err := os.Stat("harness.md"); err == nil {
		return fmt.Errorf("harness.md already exists — aborting to avoid overwriting")
	}
	if _, err := os.Stat("harness.yaml"); err == nil {
		return fmt.Errorf("harness.yaml already exists — aborting to avoid overwriting")
	}

	// Find the core directory
	// Try multiple possible locations: relative to binary, relative to cwd
	coreDirs := []string{
		filepath.Join(filepath.Dir(os.Args[0]), "..", "core"),
		filepath.Join(filepath.Dir(os.Args[0]), "core"),
		"core",
	}

	var coreDir string
	for _, dir := range coreDirs {
		if _, err := os.Stat(dir); err == nil {
			coreDir = dir
			break
		}
	}

	if coreDir != "" {
		// Copy core harness structure
		if err := copyDir(filepath.Join(coreDir, ".harness"), ".harness"); err != nil {
			return fmt.Errorf("copying core .harness: %w", err)
		}

		// Copy and customize harness.md
		coreHarness := filepath.Join(coreDir, "harness.md")
		content, err := os.ReadFile(coreHarness)
		if err != nil {
			return fmt.Errorf("reading core harness.md: %w", err)
		}

		// Replace placeholder with project name
		customized := strings.Replace(string(content), "AI Assistant", name+" Agent", 1)

		if err := os.WriteFile("harness.md", []byte(customized), 0644); err != nil {
			return fmt.Errorf("writing harness.md: %w", err)
		}
	} else {
		// No core directory found — initialize from embedded starter files
		if err := initFromStarter(name); err != nil {
			return err
		}
	}

	fmt.Printf("✅ Initialized harness project: %s\n\n", name)
	fmt.Println("Created:")
	fmt.Println("  harness.md                       Main configuration")
	fmt.Println("  .harness/tools/read_file.md      Starter tool: read_file")
	fmt.Println("  .harness/tools/write_file.md     Starter tool: write_file")
	fmt.Println("  .harness/hooks/safety.md         Safety hook: block dangerous commands")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  harness validate     # verify configuration")
	fmt.Println("  harness tools        # list available tools")
	fmt.Println("  harness run          # start interactive session")
	fmt.Println("  harness deploy       # non-interactive run (CI/CD)")

	return nil
}

// initFromStarter creates a minimal self-defining harness using embedded starter content.
// Used when no core/ directory is available (e.g., after a binary install).
func initFromStarter(name string) error {
	// Create .harness subdirectories
	for _, sub := range []string{
		filepath.Join(".harness", "tools"),
		filepath.Join(".harness", "hooks"),
		filepath.Join(".harness", "agents"),
	} {
		if err := os.MkdirAll(sub, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	// Write harness.md from embedded starter content
	harnessContent := strings.ReplaceAll(starterHarnessMD, "{{NAME}}", name)
	if err := os.WriteFile("harness.md", []byte(harnessContent), 0644); err != nil {
		return fmt.Errorf("writing harness.md: %w", err)
	}

	// Write starter tool definitions
	if err := os.WriteFile(filepath.Join(".harness", "tools", "read_file.md"), []byte(starterDefaultTools), 0644); err != nil {
		return fmt.Errorf("writing read_file tool: %w", err)
	}
	if err := os.WriteFile(filepath.Join(".harness", "tools", "write_file.md"), []byte(starterWriteFileTool), 0644); err != nil {
		return fmt.Errorf("writing write_file tool: %w", err)
	}

	// Write starter hook
	if err := os.WriteFile(filepath.Join(".harness", "hooks", "safety.md"), []byte(starterDefaultHook), 0644); err != nil {
		return fmt.Errorf("writing safety hook: %w", err)
	}

	return nil
}

// copyDir recursively copies a directory tree
func copyDir(src, dst string) error {
	// Get source dir info
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	// Read directory contents
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	// Copy each entry
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Preserve permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}
