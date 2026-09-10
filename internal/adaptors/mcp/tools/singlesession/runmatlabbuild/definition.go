// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild

const (
	name        = "run_matlab_build"
	title       = "Run MATLAB Build"
	description = "Trigger the MATLAB Build Tool for a build file (`buildfile_path`) in an existing MATLAB session. The build file must exist and be a valid .m file. This prototype returns immediately after scheduling the build in Go; it does not report MATLAB build completion, task results, cancellation, or live progress."
)

type Args struct {
	BuildfilePath string `json:"buildfile_path" jsonschema:"The full absolute path to the MATLAB Build Tool build file to run. Must be an existing .m file. Example: C:\\Users\\username\\project\\buildfile.m or /home/user/project/buildfile.m."`
}

type ReturnArgs struct {
	Message                 string `json:"message" jsonschema:"Confirmation that the build goroutine was scheduled. This does not confirm MATLAB accepted or completed the build."`
	BuildGoroutineStarted   bool   `json:"build_goroutine_started" jsonschema:"Whether the Go routine that invokes MATLAB was scheduled."`
	ProgressStreamConnected bool   `json:"progress_stream_connected" jsonschema:"Whether a live MATLAB build progress stream is connected."`
	ProgressStreamError     string `json:"progress_stream_error" jsonschema:"Explanation when a live MATLAB build progress stream is unavailable."`
}
