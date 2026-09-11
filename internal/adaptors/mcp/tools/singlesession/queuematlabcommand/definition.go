// Copyright 2026 The MathWorks, Inc.

package queuematlabcommand

const (
	name        = "queue_matlab_command"
	title       = "Queue MATLAB Command"
	description = "Queues MATLAB code for asynchronous FIFO execution in the single MATLAB session. Returns immediately without contacting MATLAB. Use poll_matlab_commands to check progress and retrieve output. WARNING: Do not queue restoredefaultpath because it removes MCP server functions from the MATLAB path."
)

type Args struct {
	Code        string `json:"code"                   jsonschema:"MATLAB code to queue for asynchronous execution."`
	ProjectPath string `json:"project_path,omitempty" jsonschema:"(Optional) Absolute path to the project folder. When provided, MATLAB sets this as the current working folder before execution. Example: C:\\Users\\username\\matlab-project or /home/user/research."`
}

type ReturnArgs struct {
	CommandID string `json:"command_id" jsonschema:"The stable identifier assigned to the queued command."`
	Status    string `json:"status"     jsonschema:"The initial command status, always queued."`
	Message   string `json:"message"    jsonschema:"A message confirming that the command was queued."`
}
