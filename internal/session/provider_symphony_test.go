package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSymphonyProvider_LoadsRuntimeRowsAndPrefersCurrentPhaseSession(t *testing.T) {
	base := time.Date(2026, 6, 22, 19, 0, 0, 0, time.UTC)
	root := t.TempDir()
	statePath := filepath.Join(root, "runtime-state.json")
	sessionsPath := filepath.Join(root, "symphony-sessions.log")
	copilotDir := filepath.Join(root, ".copilot")
	workspacesRoot := filepath.Join(root, "workspaces")

	writeCopilotSession(t, copilotDir, "impl-session", filepath.Join(workspacesRoot, "SCC-4593130"), base)

	runtimeJSON := fmt.Sprintf(`{
		"GeneratedAt": %q,
		"OrchestratorPid": 123,
		"RunningCount": 0,
		"RetryingCount": 0,
		"Running": [],
		"Retrying": [],
		"Known": [{
			"IssueId": "4593130",
			"Identifier": "SCC-4593130",
			"Title": "Implement ACS endpoints",
			"LastPhase": "Implementing",
			"Status": "Idle",
			"LastEvent": "turn_completed",
			"LastActivity": "assistant.turn_end",
			"LastUpdate": %q,
			"LatestSessionId": "planning-session",
			"PhaseSessionId": "impl-session",
			"CurrentSessionId": "impl-session",
			"SessionIdsByPhase": {
				"Planning": "planning-session",
				"Implementing": "impl-session"
			}
		}]
	}`, base.Format(time.RFC3339Nano), base.Add(time.Minute).Format(time.RFC3339Nano))
	if err := os.WriteFile(statePath, []byte(runtimeJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionsPath, []byte(strings.Join([]string{
		"2026-06-22T19:00:00Z issue=SCC-4593130 (4593130) phase=Planning session=planning-session",
		"2026-06-22T19:01:00Z issue=SCC-4593130 (4593130) phase=Implementing session=impl-session",
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	provider := NewSymphonyProvider(SymphonyConfig{
		StatePath:      statePath,
		SessionsPath:   sessionsPath,
		WorkspacesRoot: workspacesRoot,
		CopilotDir:     copilotDir,
	})
	paths, err := provider.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 row, got %v", paths)
	}

	state, err := provider.LoadSession(paths[0], State{})
	if err != nil {
		t.Fatal(err)
	}
	if state.Provider != "symphony" {
		t.Fatalf("Provider: got %q", state.Provider)
	}
	if state.SessionID != "impl-session" {
		t.Fatalf("SessionID: got %q, want impl-session", state.SessionID)
	}
	if !strings.Contains(state.ProjectName, "SCC-4593130") || !strings.Contains(state.ProjectName, "Implement ACS endpoints") {
		t.Fatalf("ProjectName missing WI details: %q", state.ProjectName)
	}
	if state.Cwd != filepath.Join(workspacesRoot, "SCC-4593130") {
		t.Fatalf("Cwd: got %q", state.Cwd)
	}
	if state.CurrentAction != "Reading main.go" {
		t.Fatalf("CurrentAction: got %q", state.CurrentAction)
	}
	if state.Status != StatusToolUse {
		t.Fatalf("Status: got %s", state.Status)
	}
	if state.MCPStatus != "ADO ?" {
		t.Fatalf("MCPStatus: got %q", state.MCPStatus)
	}
}

func TestSymphonyScanner_IncludesRowsWithoutLiveProcess(t *testing.T) {
	base := time.Date(2026, 6, 22, 19, 0, 0, 0, time.UTC)
	root := t.TempDir()
	statePath := filepath.Join(root, "runtime-state.json")
	runtimeJSON := fmt.Sprintf(`{
		"GeneratedAt": %q,
		"OrchestratorPid": 123,
		"Running": [],
		"Retrying": [],
		"Known": [{
			"IssueId": "4596942",
			"Identifier": "SCC-4596942",
			"Title": "Make Symphony dashboard interactive",
			"LastPhase": "Planning",
			"Status": "Idle",
			"LastUpdate": %q
		}]
	}`, base.Format(time.RFC3339Nano), base.Format(time.RFC3339Nano))
	if err := os.WriteFile(statePath, []byte(runtimeJSON), 0644); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerWithProvider(NewSymphonyProvider(SymphonyConfig{
		StatePath:      statePath,
		WorkspacesRoot: filepath.Join(root, "workspaces"),
		CopilotDir:     filepath.Join(root, ".copilot"),
	}))
	if err := scanner.Discover(); err != nil {
		t.Fatal(err)
	}
	scanner.LoadAll()

	rows := scanner.RunningSessions()
	if len(rows) != 1 {
		t.Fatalf("expected 1 visible Symphony row without PID, got %d", len(rows))
	}
	if rows[0].PID != 0 {
		t.Fatalf("expected no PID, got %d", rows[0].PID)
	}
	if rows[0].Status != StatusIdle {
		t.Fatalf("expected idle row, got %s", rows[0].Status)
	}
}

func TestSymphonyProvider_RunningRowFallsBackToLatestSessionId(t *testing.T) {
	base := time.Date(2026, 7, 2, 7, 45, 0, 0, time.UTC)
	root := t.TempDir()
	statePath := filepath.Join(root, "runtime-state.json")
	runtimeJSON := fmt.Sprintf(`{
		"GeneratedAt": %q,
		"OrchestratorPid": 123,
		"Running": [{
			"IssueId": "4609444",
			"IssueIdentifier": "SCC-4609444",
			"Phase": "Implementing",
			"WorkspacePath": %q,
			"SessionId": ""
		}],
		"Retrying": [],
		"Known": [{
			"IssueId": "4609444",
			"Identifier": "SCC-4609444",
			"Title": "Admin net8 Cosmic pods crash-loop",
			"LastPhase": "Implementing",
			"Status": "Running",
			"LastUpdate": %q,
			"LatestSessionId": "latest-session"
		}]
	}`, base.Format(time.RFC3339Nano), filepath.Join(root, "workspaces", "SCC-4609444"), base.Format(time.RFC3339Nano))
	if err := os.WriteFile(statePath, []byte(runtimeJSON), 0644); err != nil {
		t.Fatal(err)
	}

	provider := NewSymphonyProvider(SymphonyConfig{
		StatePath:      statePath,
		WorkspacesRoot: filepath.Join(root, "workspaces"),
		CopilotDir:     filepath.Join(root, ".copilot"),
	})
	state, err := provider.LoadSession(statePath+"#4609444", State{})
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "latest-session" {
		t.Fatalf("SessionID: got %q, want latest-session", state.SessionID)
	}
}

func TestSymphonyProvider_RetryRowsShowRetryStatus(t *testing.T) {
	base := time.Date(2026, 6, 22, 19, 0, 0, 0, time.UTC)
	root := t.TempDir()
	statePath := filepath.Join(root, "runtime-state.json")
	runtimeJSON := fmt.Sprintf(`{
		"GeneratedAt": %q,
		"OrchestratorPid": 123,
		"Running": [],
		"Retrying": [{
			"IssueId": "4599270",
			"Identifier": "SCC-4599270",
			"Attempt": 6,
			"DueAt": %q,
			"Error": "agency exited 1 without result event"
		}],
		"Known": [{
			"IssueId": "4599270",
			"Identifier": "SCC-4599270",
			"Title": "Implement validation retry loop",
			"LastPhase": "Planning",
			"Status": "Retry",
			"LastUpdate": %q
		}]
	}`, base.Format(time.RFC3339Nano), base.Add(time.Minute).Format(time.RFC3339Nano), base.Format(time.RFC3339Nano))
	if err := os.WriteFile(statePath, []byte(runtimeJSON), 0644); err != nil {
		t.Fatal(err)
	}

	provider := NewSymphonyProvider(SymphonyConfig{
		StatePath:      statePath,
		WorkspacesRoot: filepath.Join(root, "workspaces"),
		CopilotDir:     filepath.Join(root, ".copilot"),
	})
	paths, err := provider.Discover()
	if err != nil {
		t.Fatal(err)
	}
	state, err := provider.LoadSession(paths[0], State{})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusRetry {
		t.Fatalf("expected Retry status, got %s", state.Status)
	}
	if !strings.Contains(state.CurrentAction, "agency exited 1") {
		t.Fatalf("expected retry error in action, got %q", state.CurrentAction)
	}
}

func TestSymphonyProvider_MatchProcessesUsesCopilotLockFiles(t *testing.T) {
	root := t.TempDir()
	copilotDir := filepath.Join(root, ".copilot")
	sessionDir := filepath.Join(copilotDir, "session-state", "sess-1")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(sessionDir, "inuse.321.lock"), []byte("321"), 0644); err != nil {
		t.Fatal(err)
	}

	state := &State{Provider: "symphony", SessionID: "sess-1", Status: StatusResponding}
	sessions := map[string]*State{"symphony#1": state}
	provider := NewSymphonyProvider(SymphonyConfig{CopilotDir: copilotDir})
	provider.MatchProcesses(sessions, []ProcessInfo{{PID: 321, SessionID: "sess-1"}}, nil)

	if state.PID != 321 {
		t.Fatalf("PID: got %d, want 321", state.PID)
	}
}

func TestDetectAzureDevOpsMCPStatus(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "loaded connected",
			body: `{"type":"session.mcp_servers_loaded","data":{"servers":[{"name":"azure-devops","status":"connected"}]}}`,
			want: "ADO ok",
		},
		{
			name: "status changed connected",
			body: `{"type":"session.mcp_server_status_changed","data":{"serverName":"azure-devops","status":"connected"}}`,
			want: "ADO ok",
		},
		{
			name: "tool use proves availability",
			body: `{"type":"tool.execution_start","data":{"toolName":"azure-devops-wit_get_work_item","mcpServerName":"azure-devops"}}`,
			want: "ADO used",
		},
		{
			name: "loaded but missing ado",
			body: `{"type":"session.mcp_servers_loaded","data":{"servers":[{"name":"github-mcp-server","status":"connected"}]}}`,
			want: "ADO missing",
		},
		{
			name: "no mcp evidence",
			body: `{"type":"assistant.message","data":{"content":"hello"}}`,
			want: "ADO ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(path, []byte(tt.body+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if got := detectAzureDevOpsMCPStatus(path); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func writeCopilotSession(t *testing.T, copilotDir, sessionID, cwd string, base time.Time) {
	t.Helper()
	sessionDir := filepath.Join(copilotDir, "session-state", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	workspace := fmt.Sprintf("id: %s\ncwd: %s\nrepository: symphony\ncreated_at: %s\nupdated_at: %s\n",
		sessionID,
		cwd,
		base.Format(time.RFC3339Nano),
		base.Add(time.Second).Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(sessionDir, "workspace.yaml"), []byte(workspace), 0644); err != nil {
		t.Fatal(err)
	}
	events := "" +
		fmt.Sprintf(`{"type":"session.start","id":"e1","timestamp":"%s","data":{"sessionId":"%s","startTime":"%s"}}`, base.Format(time.RFC3339Nano), sessionID, base.Format(time.RFC3339Nano)) + "\n" +
		fmt.Sprintf(`{"type":"user.message","id":"e2","timestamp":"%s","data":{"content":"Implement this work item"}}`, base.Add(time.Second).Format(time.RFC3339Nano)) + "\n" +
		fmt.Sprintf(`{"type":"tool.execution_start","id":"e3","timestamp":"%s","data":{"toolName":"Read","arguments":{"file_path":"main.go"}}}`, base.Add(2*time.Second).Format(time.RFC3339Nano)) + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "events.jsonl"), []byte(events), 0644); err != nil {
		t.Fatal(err)
	}
}
