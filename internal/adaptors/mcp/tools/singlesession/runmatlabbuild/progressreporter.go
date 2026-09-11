// Copyright 2026 The MathWorks, Inc.

package runmatlabbuild

import (
	"context"

	"github.com/matlab/matlab-mcp-server/internal/usecases/runmatlabbuildfile"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProgressNotificationSender sends progress notifications to an MCP client.
type ProgressNotificationSender interface {
	NotifyProgress(ctx context.Context, params *mcp.ProgressNotificationParams) error
}

// MCPProgressReporter converts transport-neutral build progress updates to MCP progress notifications.
type MCPProgressReporter struct {
	progressNotificationSender ProgressNotificationSender
	progressToken              any
}

// NewMCPProgressReporter creates a reporter for the current MCP tool call.
func NewMCPProgressReporter(progressNotificationSender ProgressNotificationSender, progressToken any) *MCPProgressReporter {
	return &MCPProgressReporter{
		progressNotificationSender: progressNotificationSender,
		progressToken:              progressToken,
	}
}

// ReportProgress sends a build progress update through MCP.
func (r *MCPProgressReporter) ReportProgress(ctx context.Context, update runmatlabbuildfile.BuildProgressUpdate) error {
	return r.progressNotificationSender.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		Message:       update.Message,
		ProgressToken: r.progressToken,
		Progress:      update.Progress,
		Total:         update.Total,
	})
}
