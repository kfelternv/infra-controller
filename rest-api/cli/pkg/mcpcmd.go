// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	cli "github.com/urfave/cli/v2"
)

// mcpBinaryName is the standalone MCP server binary the CLI delegates to.
const mcpBinaryName = "nico-mcp"

// MCPCommand returns the "mcp" command tree. The MCP server itself lives in a
// separate binary (nico-mcp) so that neither the MCP server code nor its MCP
// SDK dependency are linked into the CLI binary. `nicocli mcp serve` locates
// that binary and execs it, forwarding the server flags plus the root-level
// connection flags (--base-url, --org, --api-name, --token, --token-command,
// --debug) that the user set.
func MCPCommand() *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "Run the NICo MCP server (delegates to the standalone nico-mcp binary)",
		Subcommands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start the streamable-HTTP MCP server via the nico-mcp binary",
				Description: "Locates the nico-mcp binary (NICO_MCP_BIN, next to nicocli, or on\n" +
					"PATH) and execs it. The MCP server runs as its own process; the MCP\n" +
					"code is intentionally not compiled into nicocli. Root-level connection\n" +
					"flags must precede the subcommand, e.g.\n" +
					"  nicocli --org tester mcp serve --listen :8080",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "listen",
						Usage:   "address:port to listen on",
						EnvVars: []string{"NICO_MCP_LISTEN"},
					},
					&cli.StringFlag{
						Name:    "path",
						Usage:   "HTTP path the MCP handler is mounted at",
						EnvVars: []string{"NICO_MCP_PATH"},
					},
					&cli.DurationFlag{
						Name:    "shutdown-timeout",
						Usage:   "graceful shutdown timeout when SIGINT/SIGTERM arrives",
						EnvVars: []string{"NICO_MCP_SHUTDOWN_TIMEOUT"},
					},
				},
				Action: runMCPServe,
			},
		},
	}
}

// runMCPServe resolves the nico-mcp binary and replaces the current process
// with it (via execve), forwarding only the flags the user explicitly set so
// nico-mcp applies its own defaults for the rest.
func runMCPServe(c *cli.Context) error {
	bin, err := resolveMCPBinary()
	if err != nil {
		return err
	}

	args := []string{bin}
	for _, name := range []string{"listen", "path", "base-url", "org", "api-name", "token", "token-command"} {
		if c.IsSet(name) {
			args = append(args, "--"+name, c.String(name))
		}
	}
	if c.IsSet("shutdown-timeout") {
		args = append(args, "--shutdown-timeout", c.Duration("shutdown-timeout").String())
	}
	if c.Bool("debug") {
		args = append(args, "--debug")
	}

	// Replace the CLI process with nico-mcp so stdio, signals, and the exit
	// code pass through transparently.
	if err := syscall.Exec(bin, args, os.Environ()); err != nil {
		return fmt.Errorf("launching %s: %w", mcpBinaryName, err)
	}
	return nil
}

// resolveMCPBinary finds the nico-mcp binary, preferring an explicit override,
// then a sibling of the running CLI, then PATH.
func resolveMCPBinary() (string, error) {
	if override := os.Getenv("NICO_MCP_BIN"); override != "" {
		return override, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), mcpBinaryName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath(mcpBinaryName); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%s binary not found: install it (e.g. `make nico-mcp`), put it next to nicocli, or set NICO_MCP_BIN", mcpBinaryName)
}
