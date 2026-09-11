// Copyright 2026 The MathWorks, Inc.

package runmatlabbuildfile

import "time"

func (m *ProgressMonitor) SetHeartbeatInterval(interval time.Duration) {
	m.heartbeatInterval = interval
}
