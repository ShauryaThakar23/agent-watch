// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

package ui

import "log"

// Dashboard-shortcut diagnostics are ALWAYS ON. Output goes to the standard
// logger, which main.redirectLog() points at <UserCacheDir>/agent-watch/
// agent-watch.log (never the alt-screen). The o/b handlers fire only on a
// keypress, so this is low volume and safe to ship enabled by default — users
// running Symphony from master capture it with no extra flags.

// udbg writes an unconditional "[ui-debug]" line to the redirected file logger.
func udbg(format string, args ...any) {
	log.Printf("[ui-debug] "+format, args...)
}
