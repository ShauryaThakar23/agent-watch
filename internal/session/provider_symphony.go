// Copyright 2026 Tarik Guney
// Licensed under the MIT License.
// https://github.com/ShauryaThakar23/agent-watch

package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ShauryaThakar23/agent-watch/internal/tmux"
)

// SymphonyConfig configures the Symphony provider.
type SymphonyConfig struct {
	StatePath      string
	SessionsPath   string
	WorkspacesRoot string
	CopilotDir     string
}

type symphonyProvider struct {
	cfg     SymphonyConfig
	copilot Provider
}

type symphonyRuntimeState struct {
	GeneratedAt     time.Time            `json:"GeneratedAt"`
	OrchestratorPid int                  `json:"OrchestratorPid"`
	Running         []symphonyRunningRow `json:"Running"`
	Retrying        []symphonyRetryRow   `json:"Retrying"`
	Known           []symphonyKnownRow   `json:"Known"`
}

type symphonyRunningRow struct {
	IssueId         string     `json:"IssueId"`
	IssueIdentifier string     `json:"IssueIdentifier"`
	State           string     `json:"State"`
	SessionId       string     `json:"SessionId"`
	Phase           string     `json:"Phase"`
	WorkspacePath   string     `json:"WorkspacePath"`
	TurnCount       int        `json:"TurnCount"`
	LastEvent       string     `json:"LastEvent"`
	StartedAt       time.Time  `json:"StartedAt"`
	LastEventAt     *time.Time `json:"LastEventAt"`
	OutputTokens    int64      `json:"OutputTokens"`
}

type symphonyRetryRow struct {
	IssueId    string    `json:"IssueId"`
	Identifier string    `json:"Identifier"`
	Attempt    int       `json:"Attempt"`
	DueAt      time.Time `json:"DueAt"`
	Error      string    `json:"Error"`
}

type symphonyKnownRow struct {
	IssueId           string            `json:"IssueId"`
	Identifier        string            `json:"Identifier"`
	Title             string            `json:"Title"`
	LastPhase         string            `json:"LastPhase"`
	Status            string            `json:"Status"`
	LastEvent         string            `json:"LastEvent"`
	LastActivity      string            `json:"LastActivity"`
	DispatchCount     int               `json:"DispatchCount"`
	OutputTokensTotal int64             `json:"OutputTokensTotal"`
	LastUpdate        time.Time         `json:"LastUpdate"`
	RetryDueAt        *time.Time        `json:"RetryDueAt"`
	LastError         string            `json:"LastError"`
	LatestSessionId   string            `json:"LatestSessionId"`
	PhaseSessionId    string            `json:"PhaseSessionId"`
	CurrentSessionId  string            `json:"CurrentSessionId"`
	SessionIdsByPhase map[string]string `json:"SessionIdsByPhase"`
}

type symphonySessionRecord struct {
	Identifier string
	IssueId    string
	Phase      string
	SessionId  string
}

var symphonySessionLineRe = regexp.MustCompile(`^\S+\s+issue=(\S+)\s+\(([^)]+)\)\s+phase=(\S+)\s+session=([\w-]+)\s*$`)

// NewSymphonyProvider creates a provider that renders Symphony work items as
// first-class dashboard rows while enriching active rows with Copilot session data.
func NewSymphonyProvider(cfg SymphonyConfig) Provider {
	return &symphonyProvider{
		cfg:     cfg,
		copilot: NewCopilotProvider(cfg.CopilotDir),
	}
}

func (p *symphonyProvider) ID() string { return "symphony" }

func (p *symphonyProvider) BaseDir() string { return filepath.Dir(p.cfg.StatePath) }

func (p *symphonyProvider) SessionsDir() string { return filepath.Dir(p.cfg.StatePath) }

func (p *symphonyProvider) RefreshExistingOnTick() bool { return true }

func (p *symphonyProvider) DiscoverOnTick() bool { return true }

func (p *symphonyProvider) ReplaceDiscoveredSessions() bool { return true }

func (p *symphonyProvider) IncludeInactiveSessions() bool { return true }

func (p *symphonyProvider) Discover() ([]string, error) {
	state, err := p.readRuntimeState()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	ids := make(map[string]struct{})
	for _, row := range state.Known {
		if row.IssueId != "" {
			ids[row.IssueId] = struct{}{}
		}
	}
	for _, row := range state.Running {
		if row.IssueId != "" {
			ids[row.IssueId] = struct{}{}
		}
	}
	for _, row := range state.Retrying {
		if row.IssueId != "" {
			ids[row.IssueId] = struct{}{}
		}
	}

	paths := make([]string, 0, len(ids))
	for id := range ids {
		paths = append(paths, p.syntheticPath(id))
	}
	sort.Strings(paths)
	return paths, nil
}

func (p *symphonyProvider) LoadSession(path string, current State) (State, error) {
	return p.loadWorkItem(path, current)
}

func (p *symphonyProvider) UpdateSession(path string, current State) (State, error) {
	return p.loadWorkItem(path, current)
}

func (p *symphonyProvider) ListProcesses() ([]ProcessInfo, error) {
	return p.copilot.ListProcesses()
}

func (p *symphonyProvider) MatchProcesses(sessions map[string]*State, procs []ProcessInfo, paneMap map[int]tmux.PaneInfo) {
	live := make(map[int]ProcessInfo, len(procs))
	for _, proc := range procs {
		live[proc.PID] = proc
	}

	for _, state := range sessions {
		state.PID = 0
		state.TmuxSession = ""
		state.TmuxPaneID = ""
		state.TmuxSendTarget = ""
		if state.SessionID == "" {
			continue
		}

		sessionDir := filepath.Join(p.cfg.CopilotDir, "session-state", state.SessionID)
		for _, lockPID := range findCopilotLockPIDs(sessionDir) {
			proc, ok := live[lockPID]
			if !ok {
				continue
			}
			state.PID = proc.PID
			tmuxSession, tmuxPaneID, tmuxSendTarget := tmux.Resolve(paneMap, proc.ParentPIDs)
			state.TmuxSession = tmuxSession
			state.TmuxPaneID = tmuxPaneID
			state.TmuxSendTarget = tmuxSendTarget
			break
		}
	}
}

func (p *symphonyProvider) loadWorkItem(path string, current State) (State, error) {
	issueID := p.issueIDFromSyntheticPath(path)
	if issueID == "" {
		return current, fmt.Errorf("invalid Symphony synthetic path %q", path)
	}

	runtimeState, err := p.readRuntimeState()
	if err != nil {
		return current, err
	}

	known := findKnown(runtimeState.Known, issueID)
	running := findRunning(runtimeState.Running, issueID)
	retry := findRetry(runtimeState.Retrying, issueID)
	records := p.readSessionRecords()

	state := current
	state.FilePath = path
	state.Provider = "symphony"
	state.SessionID = resolveSymphonySessionID(known, running, records)
	state.Cwd = resolveSymphonyWorkspace(p.cfg.WorkspacesRoot, known, running, issueID)
	state.ProjectName = symphonyProjectName(known, running, issueID)
	state.OriginalTask = symphonyOriginalTask(known, issueID)
	state.LastPrompt = state.OriginalTask
	state.LastResponse = symphonyLastResponse(known)
	state.CurrentAction = symphonyCurrentAction(known, running, retry)
	state.Status = symphonyStatus(known, running, retry)
	state.StartTime = symphonyStartTime(known, running)
	state.LastUpdate = symphonyLastUpdate(runtimeState, known, running)
	state.FileModTime = state.LastUpdate

	p.enrichFromCopilot(&state)
	if running != nil {
		// Symphony is authoritative for "running"; keep active-looking status even if
		// Copilot's tail is between events and reports idle.
		if state.Status == StatusIdle || state.Status == StatusDone || state.Status == StatusCompletedAgo {
			state.Status = StatusResponding
		}
	}
	return state, nil
}

func (p *symphonyProvider) enrichFromCopilot(state *State) {
	if state.SessionID == "" {
		return
	}
	eventsPath := filepath.Join(p.cfg.CopilotDir, "session-state", state.SessionID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		return
	}

	copilotState, err := p.copilot.LoadSession(eventsPath, State{FilePath: eventsPath, PID: state.PID})
	if err != nil {
		return
	}
	if copilotState.CurrentAction != "" {
		state.CurrentAction = copilotState.CurrentAction
	}
	if copilotState.LastPrompt != "" {
		state.LastPrompt = copilotState.LastPrompt
	}
	if copilotState.LastResponse != "" {
		state.LastResponse = copilotState.LastResponse
	}
	if copilotState.Model != "" {
		state.Model = copilotState.Model
	}
	if !copilotState.StartTime.IsZero() && state.StartTime.IsZero() {
		state.StartTime = copilotState.StartTime
	}
	if copilotState.Status != "" {
		state.Status = copilotState.Status
	}
}

func (p *symphonyProvider) readRuntimeState() (symphonyRuntimeState, error) {
	var state symphonyRuntimeState
	data, err := os.ReadFile(p.cfg.StatePath)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func (p *symphonyProvider) readSessionRecords() []symphonySessionRecord {
	if p.cfg.SessionsPath == "" {
		return nil
	}
	data, err := os.ReadFile(p.cfg.SessionsPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	start := 0
	if len(lines) > 10000 {
		start = len(lines) - 10000
	}
	records := make([]symphonySessionRecord, 0)
	for _, line := range lines[start:] {
		match := symphonySessionLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 5 {
			continue
		}
		records = append(records, symphonySessionRecord{
			Identifier: match[1],
			IssueId:    match[2],
			Phase:      match[3],
			SessionId:  match[4],
		})
	}
	return records
}

func (p *symphonyProvider) syntheticPath(issueID string) string {
	return p.cfg.StatePath + "#" + issueID
}

func (p *symphonyProvider) issueIDFromSyntheticPath(path string) string {
	_, issueID, ok := strings.Cut(path, "#")
	if !ok {
		return ""
	}
	return strings.TrimSpace(issueID)
}

func findKnown(rows []symphonyKnownRow, issueID string) *symphonyKnownRow {
	for i := range rows {
		if rows[i].IssueId == issueID {
			return &rows[i]
		}
	}
	return nil
}

func findRunning(rows []symphonyRunningRow, issueID string) *symphonyRunningRow {
	for i := range rows {
		if rows[i].IssueId == issueID {
			return &rows[i]
		}
	}
	return nil
}

func findRetry(rows []symphonyRetryRow, issueID string) *symphonyRetryRow {
	for i := range rows {
		if rows[i].IssueId == issueID {
			return &rows[i]
		}
	}
	return nil
}

func resolveSymphonySessionID(known *symphonyKnownRow, running *symphonyRunningRow, records []symphonySessionRecord) string {
	if running != nil && running.SessionId != "" {
		return running.SessionId
	}
	if known == nil {
		return ""
	}
	if known.CurrentSessionId != "" {
		return known.CurrentSessionId
	}
	if known.PhaseSessionId != "" {
		return known.PhaseSessionId
	}
	if known.LastPhase != "" {
		for i := len(records) - 1; i >= 0; i-- {
			if records[i].IssueId == known.IssueId && strings.EqualFold(records[i].Phase, known.LastPhase) {
				return records[i].SessionId
			}
		}
	}
	return known.LatestSessionId
}

func resolveSymphonyWorkspace(root string, known *symphonyKnownRow, running *symphonyRunningRow, issueID string) string {
	if running != nil && running.WorkspacePath != "" {
		return running.WorkspacePath
	}
	identifier := fmt.Sprintf("SCC-%s", issueID)
	if known != nil && known.Identifier != "" {
		identifier = known.Identifier
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, sanitizeSymphonyWorkspaceKey(identifier))
}

func sanitizeSymphonyWorkspaceKey(identifier string) string {
	replacer := strings.NewReplacer("\\", "-", "/", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return replacer.Replace(identifier)
}

func symphonyProjectName(known *symphonyKnownRow, running *symphonyRunningRow, issueID string) string {
	identifier := fmt.Sprintf("SCC-%s", issueID)
	if known != nil && known.Identifier != "" {
		identifier = known.Identifier
	} else if running != nil && running.IssueIdentifier != "" {
		identifier = running.IssueIdentifier
	}
	title := ""
	if known != nil {
		title = known.Title
	}
	if title == "" {
		return identifier
	}
	return identifier + " " + truncate(title, 80)
}

func symphonyOriginalTask(known *symphonyKnownRow, issueID string) string {
	if known == nil || known.Title == "" {
		return fmt.Sprintf("Symphony work item %s", issueID)
	}
	return known.Title
}

func symphonyLastResponse(known *symphonyKnownRow) string {
	if known == nil {
		return ""
	}
	if known.LastError != "" {
		return known.LastError
	}
	return known.LastActivity
}

func symphonyCurrentAction(known *symphonyKnownRow, running *symphonyRunningRow, retry *symphonyRetryRow) string {
	if retry != nil {
		if retry.Error != "" {
			return fmt.Sprintf("Retry %d: %s", retry.Attempt, truncate(retry.Error, 80))
		}
		return fmt.Sprintf("Retry %d due %s", retry.Attempt, retry.DueAt.Format("15:04:05"))
	}
	if known != nil && known.LastActivity != "" {
		return symphonyPhasePrefix(known.LastPhase) + known.LastActivity
	}
	if running != nil && running.LastEvent != "" {
		return symphonyPhasePrefix(running.Phase) + running.LastEvent
	}
	if known != nil && known.LastEvent != "" {
		return symphonyPhasePrefix(known.LastPhase) + known.LastEvent
	}
	return ""
}

func symphonyPhasePrefix(phase string) string {
	if phase == "" {
		return ""
	}
	return phase + ": "
}

func symphonyStatus(known *symphonyKnownRow, running *symphonyRunningRow, retry *symphonyRetryRow) Status {
	if running != nil {
		return StatusResponding
	}
	if retry != nil {
		return StatusRetry
	}
	if known == nil {
		return StatusWaiting
	}
	switch strings.ToLower(known.Status) {
	case "running":
		return StatusResponding
	case "retry":
		return StatusRetry
	case "done":
		return StatusDone
	case "quarantined", "error", "failed":
		return StatusError
	default:
		return StatusIdle
	}
}

func symphonyStartTime(known *symphonyKnownRow, running *symphonyRunningRow) time.Time {
	if running != nil && !running.StartedAt.IsZero() {
		return running.StartedAt
	}
	if known != nil && !known.LastUpdate.IsZero() {
		return known.LastUpdate
	}
	return time.Time{}
}

func symphonyLastUpdate(runtimeState symphonyRuntimeState, known *symphonyKnownRow, running *symphonyRunningRow) time.Time {
	if running != nil && running.LastEventAt != nil && !running.LastEventAt.IsZero() {
		return *running.LastEventAt
	}
	if known != nil && !known.LastUpdate.IsZero() {
		return known.LastUpdate
	}
	if !runtimeState.GeneratedAt.IsZero() {
		return runtimeState.GeneratedAt
	}
	return time.Now()
}
