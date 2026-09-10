// Copyright 2026 The MathWorks, Inc.

package getforkedmcpversion

import (
	"context"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/basetool"
	"github.com/matlab/matlab-mcp-server/internal/entities"
)

type Tool struct {
	basetool.ToolWithUnstructuredContentOutput[Args]
}

func New(
	loggerFactory basetool.LoggerFactory,
	telemetryFactory basetool.TelemetryFactory,
) *Tool {
	return &Tool{
		ToolWithUnstructuredContentOutput: basetool.NewToolWithUnstructuredContent(
			name,
			title,
			description,
			annotations.NewReadOnlyAnnotations(),
			loggerFactory,
			telemetryFactory,
			Handler(),
		),
	}
}

func Handler() basetool.HandlerWithUnstructuredContentOutput[Args] {
	return func(_ context.Context, _ entities.Logger, _ Args) (tools.RichContent, error) {
		return tools.RichContent{
			TextContent: []string{forkedMCPVersion},
		}, nil
	}
}
