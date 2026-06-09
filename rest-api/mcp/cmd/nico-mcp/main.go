// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/NVIDIA/infra-controller/rest-api/mcp/pkg/mcp"
	"github.com/NVIDIA/infra-controller/rest-api/openapi"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "nico-mcp",
		Usage: "Serve the NICo REST read surface as MCP tools over streamable-HTTP",
		Description: "Serves the NICo REST read surface as MCP tools at /mcp on the\n" +
			"configured listen address. The server is stateless and never emits\n" +
			"text/event-stream responses; every tool/call returns a single JSON\n" +
			"body. In production, place an MCP-aware gateway in front and rely on\n" +
			"the inbound Authorization header for per-call authentication.",
		Flags: mcp.ServeFlags(),
		Action: func(c *cli.Context) error {
			return mcp.Run(c, openapi.Spec)
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
