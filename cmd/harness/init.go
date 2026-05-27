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

	if coreDir == "" {
		return fmt.Errorf("core harness directory not found — tried: %v", coreDirs)
	}

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

	fmt.Printf("✅ Initialized harness project: %s\n\n", name)
	fmt.Println("Created:")
	fmt.Println("  harness.md                     Main configuration")
	fmt.Println("  .harness/tools/default-tools.md  Working tools (read_file, write_file, list_files, get_current_folder)")
	fmt.Println("  .harness/hooks/default-hooks.md  Safety hooks (command guard, secret detection)")
	fmt.Println()
	fmt.Println("Your harness is ready to use! Run:")
	fmt.Println("  harness validate     # Check configuration")
	fmt.Println("  harness tools        # List available tools")
	fmt.Println("  harness run          # Start interactive session")

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
