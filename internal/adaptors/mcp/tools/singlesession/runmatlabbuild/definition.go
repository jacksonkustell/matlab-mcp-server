// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild

const (
	name        = "run_matlab_build"
	title       = "Run MATLAB Build"
	description = "Trigger the MATLAB Build Tool for a build file (`buildfile_path`) in an existing MATLAB session. The build file must exist and be a valid .m file. When a client supplies an MCP progress token, this prototype sends three synthetic heartbeat updates at five-second intervals before returning. It does not report MATLAB build completion, task results, or cancellation."
)

type Args struct {
	BuildfilePath string `json:"buildfile_path" jsonschema:"The full absolute path to the MATLAB Build Tool build file to run. Must be an existing .m file. Example: C:\\Users\\username\\project\\buildfile.m or /home/user/project/buildfile.m."`
}

type ReturnArgs struct {
	Message       string `json:"message" jsonschema:"Summary of the build launch and progress-monitoring outcome. This does not confirm MATLAB accepted or completed the build."`
	ProgressToken any    `json:"progress_token,omitempty" jsonschema:"The MCP progress token supplied with this build request."`
}
