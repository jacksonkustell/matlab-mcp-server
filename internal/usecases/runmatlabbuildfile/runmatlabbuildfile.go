// Copyright 2026 The MathWorks, Inc.

package runmatlabbuildfile

import (
	"context"
	"fmt"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/usecases/utils/matlabstring"
)

type Args struct {
	BuildfilePath string
}

type PathValidator interface {
	ValidateMATLABScript(filePath string) (string, error)
}

type Usecase struct {
	pathValidator PathValidator
}

func New(
	pathValidator PathValidator,
) *Usecase {
	return &Usecase{
		pathValidator: pathValidator,
	}
}

// BuildEvalRequest builds the MATLAB evaluation request for a validated buildfile path.
func BuildEvalRequest(args Args) entities.EvalRequest {
	return entities.EvalRequest{
		Code: fmt.Sprintf("buildtool('-buildFile', '%s')", matlabstring.EscapeSingleQuotes(args.BuildfilePath)),
	}
}

// Execute validates the buildfile before scheduling its build in the existing MATLAB session.
// A successful return only confirms that the Go goroutine was scheduled.
func (u *Usecase) Execute(ctx context.Context, sessionLogger entities.Logger, client entities.MATLABSessionClient, request Args) error {
	validatedPath, err := u.pathValidator.ValidateMATLABScript(request.BuildfilePath)
	if err != nil {
		return err
	}

	buildRequest := BuildEvalRequest(Args{
		BuildfilePath: validatedPath,
	})
	backgroundContext := context.WithoutCancel(ctx)

	go func() {
		_, err := client.Eval(backgroundContext, sessionLogger, buildRequest)
		if err != nil {
			sessionLogger.WithError(err).Error("MATLAB build evaluation failed")
		}
	}()

	return nil
}
