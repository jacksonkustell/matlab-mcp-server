// Copyright 2026 The MathWorks, Inc.

package queuematlabcommand

import (
	"context"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/basetool"
	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
)

type Usecase interface {
	Enqueue(code string, projectPath string) (matlabcommandqueue.EnqueueResult, error)
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
			annotations.NewDestructiveAnnotations(),
			loggerFactory,
			telemetryFactory,
			Handler(usecase),
		),
	}
}

func Handler(usecase Usecase) basetool.HandlerWithStructuredContentOutput[Args, ReturnArgs] {
	return func(_ context.Context, sessionLogger entities.Logger, inputs Args) (ReturnArgs, error) {
		sessionLogger.Info("Queueing MATLAB command")
		defer sessionLogger.Info("Done - Queueing MATLAB command")

		result, err := usecase.Enqueue(inputs.Code, inputs.ProjectPath)
		if err != nil {
			return ReturnArgs{}, err
		}

		return ReturnArgs{
			CommandID: result.CommandID,
			Status:    string(result.Status),
			Message:   result.Message,
		}, nil
	}
}
