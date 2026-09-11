// Copyright 2026 The MathWorks, Inc.

package pollmatlabcommands

import "github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"

const (
	name        = "poll_matlab_commands"
	title       = "Poll MATLAB Commands"
	description = "Returns queued MATLAB commands in creation order, including their current status and any completed output. Active commands remain available for later polls. Completed and failed commands are returned once, then removed."
)

type Args struct{}

type ReturnArgs struct {
	Commands []matlabcommandqueue.Command `json:"commands" jsonschema:"All queued MATLAB commands in creation order. Terminal commands are included once and then consumed."`
}
