// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

package session

import (
	"path/filepath"
	"sync"

	"github.com/ShauryaThakar23/agent-watch/internal/tmux"
)

// Scanner orchestrates provider-specific session discovery and state tracking.
type Scanner struct {
	provider Provider
	mu       sync.RWMutex
	sessions map[string]*State // keyed by file path
}

// NewScanner creates a Scanner for Claude sessions (default provider).
func NewScanner(claudeDir string) *Scanner {
	return NewScannerWithProvider(NewClaudeProvider(claudeDir))
}

// NewScannerWithProvider creates a Scanner using the given provider.
func NewScannerWithProvider(provider Provider) *Scanner {
	return &Scanner{
		provider: provider,
		sessions: make(map[string]*State),
	}
}

// ProviderID returns the current provider identifier.
func (s *Scanner) ProviderID() string {
	return s.provider.ID()
}

// ClaudeDir returns the provider base directory for backwards compatibility.
func (s *Scanner) ClaudeDir() string {
	return s.provider.BaseDir()
}

// SessionsDir returns the session directory path for the active provider.
func (s *Scanner) SessionsDir() string {
	return s.provider.SessionsDir()
}

type sessionsDirsProvider interface {
	SessionsDirs() []string
}

type refreshExistingProvider interface {
	RefreshExistingOnTick() bool
}

type discoverOnTickProvider interface {
	DiscoverOnTick() bool
}

type replaceDiscoveredProvider interface {
	ReplaceDiscoveredSessions() bool
}

type includeInactiveProvider interface {
	IncludeInactiveSessions() bool
}

// SessionsDirs returns all session roots for the active provider.
func (s *Scanner) SessionsDirs() []string {
	if provider, ok := s.provider.(sessionsDirsProvider); ok {
		return provider.SessionsDirs()
	}
	return []string{s.provider.SessionsDir()}
}

// Discover finds session files and registers unseen ones.
func (s *Scanner) Discover() error {
	paths, err := s.provider.Discover()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if provider, ok := s.provider.(replaceDiscoveredProvider); ok && provider.ReplaceDiscoveredSessions() {
		discovered := make(map[string]struct{}, len(paths))
		for _, path := range paths {
			discovered[filepath.Clean(path)] = struct{}{}
		}
		for path := range s.sessions {
			if _, ok := discovered[filepath.Clean(path)]; !ok {
				delete(s.sessions, path)
			}
		}
	}
	for _, path := range paths {
		if _, exists := s.sessions[path]; !exists {
			s.sessions[path] = &State{FilePath: path}
		}
	}
	return nil
}

// LoadSession reads a session file and fully populates its state.
func (s *Scanner) LoadSession(path string) error {
	s.mu.RLock()
	current := State{FilePath: path}
	if st, exists := s.sessions[path]; exists {
		current = *st
	}
	s.mu.RUnlock()

	updated, err := s.provider.LoadSession(path, current)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	st, exists := s.sessions[path]
	if !exists {
		s.sessions[path] = &updated
		return nil
	}
	*st = updated
	return nil
}

// UpdateSession reads newly appended bytes and updates the cached state.
func (s *Scanner) UpdateSession(path string) error {
	s.mu.RLock()
	current, exists := s.sessions[path]
	if !exists {
		s.mu.RUnlock()
		return s.LoadSession(path)
	}
	snapshot := *current
	s.mu.RUnlock()

	updated, err := s.provider.UpdateSession(path, snapshot)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sessions[path]
	if !ok {
		s.sessions[path] = &updated
		return nil
	}
	*st = updated
	return nil
}

// Sessions returns a snapshot of all known session states.
func (s *Scanner) Sessions() []State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]State, 0, len(s.sessions))
	for _, state := range s.sessions {
		result = append(result, *state)
	}
	return result
}

// LoadAll loads sessions that haven't been initialized yet.
func (s *Scanner) LoadAll() {
	s.mu.RLock()
	refreshExisting := false
	if provider, ok := s.provider.(refreshExistingProvider); ok {
		refreshExisting = provider.RefreshExistingOnTick()
	}

	paths := make([]string, 0)
	for path, state := range s.sessions {
		if refreshExisting || state.FileOffset == 0 {
			paths = append(paths, path)
		}
	}
	s.mu.RUnlock()

	for _, path := range paths {
		_ = s.LoadSession(path)
	}
}

// DiscoverOnTick reports whether the provider needs periodic discovery even
// when fsnotify has not announced a new transcript file. Symphony rows come
// from runtime-state.json, not per-session JSONL create events, so they use this.
func (s *Scanner) DiscoverOnTick() bool {
	if provider, ok := s.provider.(discoverOnTickProvider); ok {
		return provider.DiscoverOnTick()
	}
	return false
}

// MatchProcesses associates running processes with sessions.
func (s *Scanner) MatchProcesses(procs []ProcessInfo, paneMap map[int]tmux.PaneInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider.MatchProcesses(s.sessions, procs, paneMap)
}

// RefreshProcesses discovers active provider processes and matches them to sessions.
func (s *Scanner) RefreshProcesses(paneMap map[int]tmux.PaneInfo) error {
	procs, err := s.provider.ListProcesses()
	if err != nil {
		return err
	}
	s.MatchProcesses(procs, paneMap)
	return nil
}

// RunningSessions returns sessions with a live process, deduplicated by cwd.
func (s *Scanner) RunningSessions() []State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	includeInactive := false
	if provider, ok := s.provider.(includeInactiveProvider); ok {
		includeInactive = provider.IncludeInactiveSessions()
	}

	result := make([]State, 0)
	for _, state := range s.sessions {
		if state.PID > 0 || includeInactive {
			result = append(result, *state)
		}
	}
	return deduplicateByCwd(result)
}

// deduplicateByCwd keeps only the most recently modified session per cwd.
func deduplicateByCwd(sessions []State) []State {
	best := make(map[string]State)
	for _, s := range sessions {
		key := s.Cwd
		if key == "" {
			key = s.FilePath
		}
		key = filepath.Clean(key)
		existing, ok := best[key]
		if !ok || s.FileModTime.After(existing.FileModTime) {
			best[key] = s
		}
	}
	result := make([]State, 0, len(best))
	for _, s := range best {
		result = append(result, s)
	}
	return result
}
