// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	urfave "github.com/urfave/cli/v2"
)

// ServeFlags returns the full flag set for the standalone nico-mcp server
// binary. The server-specific flags (listen, path, shutdown-timeout) sit
// alongside the connection flags (base-url, org, api-name, token, debug).
// The dynamically-generated CLI exposes the latter as
// root-level flags, but nico-mcp is a single-purpose binary with no parent
// command to inherit from, so it declares all of them itself. Each flag also
// reads its NICO_* environment variable.
func ServeFlags() []urfave.Flag {
	return []urfave.Flag{
		&urfave.StringFlag{
			Name:    "listen",
			Usage:   "address:port to listen on",
			EnvVars: []string{"NICO_MCP_LISTEN"},
			Value:   ":8080",
		},
		&urfave.StringFlag{
			Name:    "path",
			Usage:   "HTTP path the MCP handler is mounted at",
			EnvVars: []string{"NICO_MCP_PATH"},
			Value:   "/mcp",
		},
		&urfave.DurationFlag{
			Name:    "shutdown-timeout",
			Usage:   "graceful shutdown timeout when SIGINT/SIGTERM arrives",
			EnvVars: []string{"NICO_MCP_SHUTDOWN_TIMEOUT"},
			Value:   10 * time.Second,
		},
		&urfave.StringFlag{
			Name:    "base-url",
			Usage:   "default NICo REST base URL (a per-call base_url argument overrides this)",
			EnvVars: []string{"NICO_BASE_URL"},
		},
		&urfave.StringFlag{
			Name:    "org",
			Usage:   "default org used in /v2/org/<org>/... paths (a per-call org argument overrides this)",
			EnvVars: []string{"NICO_ORG"},
		},
		&urfave.StringFlag{
			Name:    "api-name",
			Usage:   "API path segment used in /v2/org/<org>/<name>/... routes",
			EnvVars: []string{"NICO_API_NAME"},
			Value:   "nico",
		},
		&urfave.StringFlag{
			Name:    "token",
			Usage:   "default bearer token (a per-call token argument or inbound Authorization header overrides this)",
			EnvVars: []string{"NICO_TOKEN"},
		},
		&urfave.BoolFlag{
			Name:  "debug",
			Usage: "enable debug logging (full HTTP request/response)",
		},
	}
}

// Run wires the urfave context into Options, builds the MCP server, and runs
// an http.Server until SIGINT/SIGTERM. It is the action backing the standalone
// nico-mcp binary, exported so the binary's main stays a thin wrapper and so
// tests can drive it directly.
func Run(c *urfave.Context, specData []byte) error {
	opts := buildServeOptions(c)

	server, err := BuildServer(specData, opts)
	if err != nil {
		return fmt.Errorf("building MCP server: %w", err)
	}

	listen := c.String("listen")
	path := c.String("path")
	if path == "" || path[0] != '/' {
		return fmt.Errorf("invalid --path %q: must be non-empty and start with '/'", path)
	}
	shutdownTimeout := c.Duration("shutdown-timeout")

	mux := http.NewServeMux()
	if err := registerHandler(mux, path, NewHandler(server)); err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	opts.Log.Infof("nico-mcp: listening on %s, MCP at %s (stateless, JSONResponse)", listen, path)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case sig := <-sigCh:
		opts.Log.Infof("nico-mcp: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

func registerHandler(mux *http.ServeMux, path string, handler http.Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid --path %q: %v", path, r)
		}
	}()
	mux.Handle(path, handler)
	return nil
}

// buildServeOptions resolves the MCP server's start-up defaults from the
// process flags (each of which also reads its NICO_* environment
// variable). Unlike the dynamically-generated CLI commands, nico-mcp does
// NOT read ~/.nico/config.yaml: the server is stateless and entirely
// parameter-driven, so every connection detail is supplied per tool call
// via resolveCallConfig, with these flag values as the only fallback.
// This lets nico-mcp start cleanly with no config file present.
func buildServeOptions(c *urfave.Context) Options {
	log := logrus.NewEntry(logrus.StandardLogger())
	if c.Bool("debug") {
		log.Logger.SetLevel(logrus.DebugLevel)
	}

	return Options{
		BaseURL: c.String("base-url"),
		Org:     c.String("org"),
		APIName: c.String("api-name"),
		Token:   c.String("token"),
		Debug:   c.Bool("debug"),
		Log:     log,
	}
}
