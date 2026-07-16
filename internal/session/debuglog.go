// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

package session

import (
	"log"
	"os"
)

// symphonyDebugEnabled turns on verbose Symphony read-path diagnostics. It is
// opt-in via AGENT_WATCH_DEBUG=1 so a normal build (including one shipped to
// master) stays quiet. When enabled, messages go to the standard logger, which
// main.redirectLog() points at <UserCacheDir>/agent-watch/agent-watch.log.
var symphonyDebugEnabled = os.Getenv("AGENT_WATCH_DEBUG") != ""

// sdbg writes a "[symphony-debug]" line to the redirected file logger when
// AGENT_WATCH_DEBUG is set; otherwise it is a no-op.
func sdbg(format string, args ...any) {
	if !symphonyDebugEnabled {
		return
	}
	log.Printf("[symphony-debug] "+format, args...)
}
