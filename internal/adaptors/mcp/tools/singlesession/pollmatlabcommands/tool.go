// Copyright 2026 The MathWorks, Inc.

package pollmatlabcommands

import (
	"context"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/basetool"
	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
)

type Usecase interface {
	Poll() []matlabcommandqueue.Command
}

type Tool struct {
	basetool.ToolWithStructuredContentOutput[Args, ReturnArgs]
}

func New(
	loggerFactory basetool.LoggerFactory,
	telemetryFactory basetool.TelemetryFactory,
	usecase Usecase,
) *Tool {
	return &Tool{
		ToolWithStructuredContentOutput: basetool.NewToolWithStructuredContent(
			name,
			title,
			description,
			annotations.NewReadOnlyAnnotations(),
			loggerFactory,
			telemetryFactory,
			Handler(usecase),
		),
	}
}

func Handler(usecase Usecase) basetool.HandlerWithStructuredContentOutput[Args, ReturnArgs] {
	return func(_ context.Context, sessionLogger entities.Logger, _ Args) (ReturnArgs, error) {
		sessionLogger.Info("Polling MATLAB commands")
		defer sessionLogger.Info("Done - Polling MATLAB commands")

		return ReturnArgs{Commands: usecase.Poll()}, nil
	}
}
