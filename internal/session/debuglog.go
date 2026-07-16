// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

package session

import (
	"log"
	"sync"
)

// Symphony read-path diagnostics are ALWAYS ON. They are written via the
// standard logger, which main.redirectLog() points at the file
// <UserCacheDir>/agent-watch/agent-watch.log — never stdout/stderr, so they do
// not corrupt the Bubble Tea alt-screen. This lets anyone running Symphony from
// master reproduce the o/b shortcut issue and share the log with no extra flags
// or env vars.

// sdbg writes an unconditional "[symphony-debug]" line to the redirected file
// logger.
func sdbg(format string, args ...any) {
	log.Printf("[symphony-debug] "+format, args...)
}

// sdbgStateChanged reports whether signature differs from the previously seen
// one, updating the stored value. readRuntimeState runs on every ~1s dashboard
// tick, so callers use this to emit their (multi-line) verbose block only when
// the parsed state actually changes — collapsing identical consecutive ticks
// into one entry while still capturing every transition.
func sdbgStateChanged(signature string) bool {
	lastSignatureMu.Lock()
	defer lastSignatureMu.Unlock()
	if signature == lastSignature {
		return false
	}
	lastSignature = signature
	return true
}

var (
	lastSignatureMu sync.Mutex
	lastSignature   string
)

