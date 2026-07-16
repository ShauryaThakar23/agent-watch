// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

package ui

import (
	"log"
	"os"
)

// uiDebugEnabled turns on verbose dashboard-shortcut diagnostics. Opt-in via
// AGENT_WATCH_DEBUG=1 so normal builds stay quiet. Output goes to the standard
// logger, which main.redirectLog() points at the agent-watch.log file.
var uiDebugEnabled = os.Getenv("AGENT_WATCH_DEBUG") != ""

// udbg writes a "[ui-debug]" line to the redirected file logger when
// AGENT_WATCH_DEBUG is set; otherwise it is a no-op.
func udbg(format string, args ...any) {
	if !uiDebugEnabled {
		return
	}
	log.Printf("[ui-debug] "+format, args...)
}
