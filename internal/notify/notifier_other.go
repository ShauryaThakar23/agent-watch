// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

//go:build !windows

package notify

// NewWindowsNotifier returns a no-op notifier outside Windows.
func NewWindowsNotifier() Notifier {
	return noopNotifier{}
}
