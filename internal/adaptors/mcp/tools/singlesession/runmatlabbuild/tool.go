// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild

import (
	"context"
	"fmt"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/basetool"
	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
)

const (
	buildTriggeredMessage         = "Build triggered successfully"
	noProgressTokenMessage        = "Build triggered successfully. No progress token was supplied, so no progress updates were sent."
	progressMonitorFailureMessage = "Build triggered successfully, but progress monitoring failed: %v"
)

type Usecase interface {
	Execute(ctx context.Context, sessionLogger entities.Logger, client entities.MATLABSessionClient, request runmatlabbuildfile.Args) error
}

type ProgressMonitor interface {
	Monitor(ctx context.Context, reporter runmatlabbuildfile.ProgressReporter) error
}

type Tool struct {
	basetool.ToolWithStructuredContentOutput[Args, ReturnArgs]
}

func New(
	loggerFactory basetool.LoggerFactory,
	telemetryFactory basetool.TelemetryFactory,
	usecase Usecase,
	globalMATLAB entities.GlobalMATLAB,
	progressMonitor ProgressMonitor,
) *Tool {
	return &Tool{
		ToolWithStructuredContentOutput: basetool.NewToolWithRequestAwareStructuredContent(name, title, description, annotations.NewDestructiveAnnotations(), loggerFactory, telemetryFactory, Handler(usecase, globalMATLAB, progressMonitor)),
	}
}

func Handler(usecase Usecase, globalMATLAB entities.GlobalMATLAB, progressMonitor ProgressMonitor) basetool.RequestAwareHandlerWithStructuredContentOutput[Args, ReturnArgs] {
	return func(ctx context.Context, sessionLogger entities.Logger, toolCall basetool.ToolCallRequest, inputs Args) (ReturnArgs, error) {
		sessionLogger.Info("Executing Run MATLAB Build tool")
		defer sessionLogger.Info("Done - Executing Run MATLAB Build tool")

		client, err := globalMATLAB.Client(ctx, sessionLogger)
		if err != nil {
			return ReturnArgs{}, err
		}

		err = usecase.Execute(ctx, sessionLogger, client, runmatlabbuildfile.Args{
			BuildfilePath: inputs.BuildfilePath,
		})
		if err != nil {
			return ReturnArgs{}, err
		}

		if toolCall.ProgressToken == nil {
			return ReturnArgs{Message: noProgressTokenMessage}, nil
		}

		reporter := NewMCPProgressReporter(toolCall.Session, toolCall.ProgressToken)
		if err := progressMonitor.Monitor(ctx, reporter); err != nil {
			return ReturnArgs{
				Message:       fmt.Sprintf(progressMonitorFailureMessage, err),
				ProgressToken: toolCall.ProgressToken,
			}, nil
		}

		return ReturnArgs{
			Message:       buildTriggeredMessage,
			ProgressToken: toolCall.ProgressToken,
		}, nil
	}
}
