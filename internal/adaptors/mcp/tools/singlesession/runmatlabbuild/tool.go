// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild

import (
	"context"

	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/annotations"
	"github.com/matlab/matlab-mcp-server/internal/adaptors/mcp/tools/basetool"
	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
)

const progressStreamUnavailable = "MATLAB progress stream is not configured in this prototype"

type Usecase interface {
	Execute(ctx context.Context, sessionLogger entities.Logger, client entities.MATLABSessionClient, request runmatlabbuildfile.Args) error
}

type Tool struct {
	basetool.ToolWithStructuredContentOutput[Args, ReturnArgs]
}

func New(
	loggerFactory basetool.LoggerFactory,
	telemetryFactory basetool.TelemetryFactory,
	usecase Usecase,
	globalMATLAB entities.GlobalMATLAB,
) *Tool {
	return &Tool{
		ToolWithStructuredContentOutput: basetool.NewToolWithStructuredContent(name, title, description, annotations.NewDestructiveAnnotations(), loggerFactory, telemetryFactory, Handler(usecase, globalMATLAB)),
	}
}

func Handler(usecase Usecase, globalMATLAB entities.GlobalMATLAB) basetool.HandlerWithStructuredContentOutput[Args, ReturnArgs] {
	return func(ctx context.Context, sessionLogger entities.Logger, inputs Args) (ReturnArgs, error) {
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

		return ReturnArgs{
			Message:                 "Build triggered successfully",
			BuildGoroutineStarted:   true,
			ProgressStreamConnected: false,
			ProgressStreamError:     progressStreamUnavailable,
		}, nil
	}
}
