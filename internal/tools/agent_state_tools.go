package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/t-rex-ai/t-rex/internal/config"
)

type MissionState struct {
	Goal        string   `json:"goal,omitempty"`
	Scope       []string `json:"scope,omitempty"`
	Phase       string   `json:"phase,omitempty"`
	DoneWhen    string   `json:"doneWhen,omitempty"`
	BlockedBy   string   `json:"blockedBy,omitempty"`
	Deliverable string   `json:"deliverable,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

type FindingRecord struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Category      string `json:"category,omitempty"`
	CWE           string `json:"cwe,omitempty"`
	OWASP         string `json:"owasp,omitempty"`
	CVSS          string `json:"cvss,omitempty"`
	Severity      string `json:"severity,omitempty"`
	Confidence    string `json:"confidence,omitempty"`
	Asset         string `json:"asset,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Parameter     string `json:"parameter,omitempty"`
	Status        string `json:"status,omitempty"`
	Prerequisites string `json:"prerequisites,omitempty"`
	Reproduction  string `json:"reproduction,omitempty"`
	Observed      string `json:"observed,omitempty"`
	Expected      string `json:"expected,omitempty"`
	Evidence      string `json:"evidence,omitempty"`
	Request       string `json:"request,omitempty"`
	Response      string `json:"response,omitempty"`
	Impact        string `json:"impact,omitempty"`
	Remediation   string `json:"remediation,omitempty"`
	Retest        string `json:"retest,omitempty"`
	Notes         string `json:"notes,omitempty"`
	UpdatedAt     string `json:"updatedAt"`
}

type EvidenceRecord struct {
	ID        string `json:"id"`
	Type      string `json:"type,omitempty"`
	Title     string `json:"title,omitempty"`
	FindingID string `json:"findingId,omitempty"`
	Path      string `json:"path,omitempty"`
	Summary   string `json:"summary,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type AssessmentPlanState struct {
	Objective     string   `json:"objective,omitempty"`
	TargetProfile string   `json:"targetProfile,omitempty"`
	TestLevel     string   `json:"testLevel,omitempty"`
	InScope       []string `json:"inScope,omitempty"`
	OutOfScope    []string `json:"outOfScope,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Rules         string   `json:"rules,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
}

// GoalState tracks an objective plus its acceptance criteria for goal mode.
type GoalState struct {
	Objective  string   `json:"objective,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	Status     string   `json:"status,omitempty"` // active | achieved | blocked | paused
	Blockers   string   `json:"blockedBy,omitempty"`
	Progress   []string `json:"progress,omitempty"`
	Iterations int      `json:"iterations,omitempty"`
	UpdatedAt  string   `json:"updatedAt,omitempty"`
}

type SecurityCoverageRecord struct {
	ID         string `json:"id"`
	CatalogID  string `json:"catalogId,omitempty"`
	Area       string `json:"area,omitempty"`
	Target     string `json:"target,omitempty"`
	Status     string `json:"status,omitempty"`
	Tested     string `json:"tested,omitempty"`
	NotTested  string `json:"notTested,omitempty"`
	Reason     string `json:"reason,omitempty"`
	EvidenceID string `json:"evidenceId,omitempty"`
	FindingID  string `json:"findingId,omitempty"`
	Notes      string `json:"notes,omitempty"`
	UpdatedAt  string `json:"updatedAt"`
}

type FindingQualityIssue struct {
	FindingID string   `json:"findingId"`
	Title     string   `json:"title,omitempty"`
	Status    string   `json:"status,omitempty"`
	Missing   []string `json:"missing"`
}

type AgentStateSnapshot struct {
	Mission        MissionState             `json:"mission,omitempty"`
	Goal           GoalState                `json:"goal,omitempty"`
	Scopes         []string                 `json:"scopes,omitempty"`
	Findings       []FindingRecord          `json:"findings,omitempty"`
	Evidence       []EvidenceRecord         `json:"evidence,omitempty"`
	AssessmentPlan AssessmentPlanState      `json:"assessmentPlan,omitempty"`
	Coverage       []SecurityCoverageRecord `json:"coverage,omitempty"`
	SecurityTests  []SecurityTestRecord     `json:"securityTests,omitempty"`
	Todos          []string                 `json:"todos,omitempty"`
}

var agentState = struct {
	sync.Mutex
	Mission        MissionState
	Goal           GoalState
	Scopes         []string
	Findings       []FindingRecord
	Evidence       []EvidenceRecord
	AssessmentPlan AssessmentPlanState
	Coverage       []SecurityCoverageRecord
	SecurityTests  []SecurityTestRecord
}{}

type MissionTool struct{}

func (t *MissionTool) Name() string { return "Mission" }
func (t *MissionTool) Description() string {
	return "Maintain the current task mission: goal, scope, phase, completion condition, blocker, and final deliverable."
}
func (t *MissionTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":      map[string]interface{}{"type": "string", "enum": []string{"set", "update", "show", "clear"}},
			"goal":        map[string]interface{}{"type": "string"},
			"scope":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"phase":       map[string]interface{}{"type": "string"},
			"done_when":   map[string]interface{}{"type": "string"},
			"blocked_by":  map[string]interface{}{"type": "string"},
			"deliverable": map[string]interface{}{"type": "string"},
		},
		"required": []string{"action"},
	}
}
func (t *MissionTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "set", "update":
		if action == "set" {
			agentState.Mission = MissionState{}
		}
		updateMissionLocked(input)
	case "clear":
		agentState.Mission = MissionState{}
	case "show", "list", "":
	default:
		return &ToolResult{Content: "unknown mission action: " + action, IsError: true}, nil
	}
	data, _ := json.MarshalIndent(agentState.Mission, "", "  ")
	return &ToolResult{Content: string(data), Output: agentState.Mission}, nil
}

type GoalTool struct{}

func (t *GoalTool) Name() string { return "Goal" }
func (t *GoalTool) Description() string {
	return "Maintain the goal-mode objective and its acceptance criteria: set the objective, add done_when criteria, append progress notes, and mark status active/achieved/blocked/paused."
}
func (t *GoalTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":    map[string]interface{}{"type": "string", "enum": []string{"set", "update", "add_criteria", "progress", "status", "show", "clear"}},
			"objective": map[string]interface{}{"type": "string"},
			"done_when": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"criteria":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"status":    map[string]interface{}{"type": "string", "enum": []string{"active", "achieved", "blocked", "paused"}},
			"blocked_by": map[string]interface{}{"type": "string"},
			"blocker":    map[string]interface{}{"type": "string"},
			"note":       map[string]interface{}{"type": "string"},
		},
		"required": []string{"action"},
	}
}
func (t *GoalTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "set":
		agentState.Goal = GoalState{}
		updateGoalLocked(input)
	case "update":
		updateGoalLocked(input)
	case "add_criteria", "add-criteria", "criteria":
		criteria := append(stringListInput(input["done_when"]), stringListInput(input["criteria"])...)
		for _, item := range nonEmptyStrings(criteria) {
			agentState.Goal.DoneWhen = appendUniqueLimit(agentState.Goal.DoneWhen, item, 100)
		}
		agentState.Goal.UpdatedAt = time.Now().Format(time.RFC3339)
	case "progress":
		if note := strings.TrimSpace(toString(input["note"])); note != "" {
			agentState.Goal.Progress = appendUniqueLimit(agentState.Goal.Progress, note, 100)
			agentState.Goal.UpdatedAt = time.Now().Format(time.RFC3339)
		}
	case "status":
		if status := normalizeChoice(toString(input["status"]), map[string]string{
			"active": "active", "in-progress": "active", "in_progress": "active", "working": "active",
			"achieved": "achieved", "done": "achieved", "complete": "achieved", "completed": "achieved",
			"blocked": "blocked", "stuck": "blocked",
			"paused": "paused", "hold": "paused",
		}); status != "" {
			agentState.Goal.Status = status
			agentState.Goal.UpdatedAt = time.Now().Format(time.RFC3339)
		}
	case "clear":
		agentState.Goal = GoalState{}
	case "show", "list", "":
	default:
		return &ToolResult{Content: "unknown goal action: " + action, IsError: true}, nil
	}
	return marshalTool(agentState.Goal)
}

func updateGoalLocked(input map[string]interface{}) {
	if v := strings.TrimSpace(toString(input["objective"])); v != "" {
		agentState.Goal.Objective = v
	}
	criteria := append(stringListInput(input["done_when"]), stringListInput(input["criteria"])...)
	for _, item := range nonEmptyStrings(criteria) {
		agentState.Goal.DoneWhen = appendUniqueLimit(agentState.Goal.DoneWhen, item, 100)
	}
	if v := strings.TrimSpace(firstNonEmptyString(toString(input["blocked_by"]), toString(input["blocker"]))); v != "" {
		agentState.Goal.Blockers = v
	}
	if v := strings.TrimSpace(toString(input["note"])); v != "" {
		agentState.Goal.Progress = appendUniqueLimit(agentState.Goal.Progress, v, 100)
	}
	if status := normalizeChoice(toString(input["status"]), map[string]string{
		"active": "active", "in-progress": "active", "in_progress": "active", "working": "active",
		"achieved": "achieved", "done": "achieved", "complete": "achieved", "completed": "achieved",
		"blocked": "blocked", "stuck": "blocked",
		"paused": "paused", "hold": "paused",
	}); status != "" {
		agentState.Goal.Status = status
	}
	if agentState.Goal.Status == "" {
		agentState.Goal.Status = "active"
	}
	agentState.Goal.UpdatedAt = time.Now().Format(time.RFC3339)
}

type ScopeTool struct{}

func (t *ScopeTool) Name() string { return "Scope" }
func (t *ScopeTool) Description() string {
	return "Track authorized scope entries for security work. Use add, list, remove, or clear."
}
func (t *ScopeTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"add", "list", "remove", "clear"}},
			"target": map[string]interface{}{"type": "string"},
			"index":  map[string]interface{}{"type": "integer"},
		},
		"required": []string{"action"},
	}
}
func (t *ScopeTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "add":
		target := strings.TrimSpace(toString(input["target"]))
		if target == "" {
			return &ToolResult{Content: "missing target", IsError: true}, nil
		}
		agentState.Scopes = appendUniqueLimit(agentState.Scopes, target, 200)
		agentState.Mission.Scope = appendUniqueLimit(agentState.Mission.Scope, target, 200)
		agentState.Mission.UpdatedAt = time.Now().Format(time.RFC3339)
	case "remove":
		idx := intNumber(input["index"]) - 1
		if idx >= 0 && idx < len(agentState.Scopes) {
			agentState.Scopes = append(agentState.Scopes[:idx], agentState.Scopes[idx+1:]...)
		}
	case "clear":
		agentState.Scopes = nil
		agentState.Mission.Scope = nil
	case "list", "":
	default:
		return &ToolResult{Content: "unknown scope action: " + action, IsError: true}, nil
	}
	data, _ := json.MarshalIndent(agentState.Scopes, "", "  ")
	return &ToolResult{Content: string(data), Output: map[string]interface{}{"scope": append([]string(nil), agentState.Scopes...)}}, nil
}

type FindingTool struct{}

func (t *FindingTool) Name() string { return "Finding" }
func (t *FindingTool) Description() string {
	return "Create, update, and list structured security findings with remediation, status, and Burp-style request/response POC evidence in the request/response fields."
}
func (t *FindingTool) InputSchema() map[string]interface{} {
	fields := map[string]interface{}{}
	for _, key := range []string{"id", "title", "category", "cwe", "owasp", "cvss", "severity", "confidence", "asset", "endpoint", "parameter", "status", "prerequisites", "reproduction", "observed", "expected", "evidence", "request", "response", "impact", "remediation", "retest", "notes"} {
		fields[key] = map[string]interface{}{"type": "string"}
	}
	fields["action"] = map[string]interface{}{"type": "string", "enum": []string{"add", "update", "list", "show", "clear"}}
	fields["severity"] = map[string]interface{}{"type": "string", "enum": []string{"critical", "high", "medium", "low", "info", "informational"}}
	fields["confidence"] = map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}}
	fields["status"] = map[string]interface{}{"type": "string", "enum": []string{"confirmed", "likely", "false-positive", "untested", "needs-dynamic-validation", "duplicate", "accepted-risk"}}
	return map[string]interface{}{"type": "object", "properties": fields, "required": []string{"action"}}
}
func (t *FindingTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "add":
		record := findingFromInput(input)
		if strings.TrimSpace(record.Title) == "" {
			return &ToolResult{Content: "missing title", IsError: true}, nil
		}
		if record.ID == "" {
			record.ID = "F-" + strings.ToUpper(uuid.New().String()[:8])
		}
		record.UpdatedAt = time.Now().Format(time.RFC3339)
		agentState.Findings = append(agentState.Findings, record)
	case "update":
		id := strings.TrimSpace(toString(input["id"]))
		if id == "" {
			return &ToolResult{Content: "missing id", IsError: true}, nil
		}
		for i := range agentState.Findings {
			if strings.EqualFold(agentState.Findings[i].ID, id) {
				mergeFinding(&agentState.Findings[i], input)
				agentState.Findings[i].UpdatedAt = time.Now().Format(time.RFC3339)
				return marshalTool(agentState.Findings[i])
			}
		}
		return &ToolResult{Content: "finding not found: " + id, IsError: true}, nil
	case "show":
		id := strings.TrimSpace(toString(input["id"]))
		for _, finding := range agentState.Findings {
			if strings.EqualFold(finding.ID, id) {
				return marshalTool(finding)
			}
		}
		return &ToolResult{Content: "finding not found: " + id, IsError: true}, nil
	case "clear":
		agentState.Findings = nil
	case "list", "":
	default:
		return &ToolResult{Content: "unknown finding action: " + action, IsError: true}, nil
	}
	return marshalTool(agentState.Findings)
}

type EvidenceTool struct {
	config *config.Config
}

func (t *EvidenceTool) Name() string { return "Evidence" }
func (t *EvidenceTool) Description() string {
	return "Persist full evidence content under .cache/evidence and keep a lightweight evidence index for reports."
}
func (t *EvidenceTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":     map[string]interface{}{"type": "string", "enum": []string{"add", "list", "show", "clear"}},
			"id":         map[string]interface{}{"type": "string"},
			"type":       map[string]interface{}{"type": "string"},
			"title":      map[string]interface{}{"type": "string"},
			"finding_id": map[string]interface{}{"type": "string"},
			"summary":    map[string]interface{}{"type": "string"},
			"content":    map[string]interface{}{"type": "string"},
		},
		"required": []string{"action"},
	}
}
func (t *EvidenceTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "add":
		content := toString(input["content"])
		if strings.TrimSpace(content) == "" {
			return &ToolResult{Content: "missing content", IsError: true}, nil
		}
		record := EvidenceRecord{
			ID:        firstNonEmptyString(toString(input["id"]), "E-"+strings.ToUpper(uuid.New().String()[:8])),
			Type:      toString(input["type"]),
			Title:     toString(input["title"]),
			FindingID: toString(input["finding_id"]),
			Summary:   toString(input["summary"]),
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		path, err := writeEvidenceFile(t.config, record.ID, content)
		if err != nil {
			return &ToolResult{Content: err.Error(), IsError: true}, nil
		}
		record.Path = path
		agentState.Evidence = append(agentState.Evidence, record)
		return marshalTool(record)
	case "show":
		id := strings.TrimSpace(toString(input["id"]))
		for _, evidence := range agentState.Evidence {
			if strings.EqualFold(evidence.ID, id) {
				return marshalTool(evidence)
			}
		}
		return &ToolResult{Content: "evidence not found: " + id, IsError: true}, nil
	case "clear":
		agentState.Evidence = nil
	case "list", "":
	default:
		return &ToolResult{Content: "unknown evidence action: " + action, IsError: true}, nil
	}
	return marshalTool(agentState.Evidence)
}

type AssessmentPlanTool struct{}

func (t *AssessmentPlanTool) Name() string { return "AssessmentPlan" }
func (t *AssessmentPlanTool) Description() string {
	return "Maintain the authorized security assessment plan: objective, target profile, in-scope and out-of-scope items, roles, and rules of engagement."
}
func (t *AssessmentPlanTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":         map[string]interface{}{"type": "string", "enum": []string{"set", "update", "show", "clear"}},
			"objective":      map[string]interface{}{"type": "string"},
			"target_profile": map[string]interface{}{"type": "string"},
			"test_level":     map[string]interface{}{"type": "string", "enum": []string{"passive", "low-noise", "active", "intrusive"}},
			"in_scope":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"out_of_scope":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"roles":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"rules":          map[string]interface{}{"type": "string"},
		},
		"required": []string{"action"},
	}
}
func (t *AssessmentPlanTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "set", "update":
		if action == "set" {
			agentState.AssessmentPlan = AssessmentPlanState{}
		}
		updateAssessmentPlanLocked(input)
	case "clear":
		agentState.AssessmentPlan = AssessmentPlanState{}
	case "show", "list", "":
	default:
		return &ToolResult{Content: "unknown assessment plan action: " + action, IsError: true}, nil
	}
	return marshalTool(agentState.AssessmentPlan)
}

type SecurityCoverageTool struct{}

func (t *SecurityCoverageTool) Name() string { return "SecurityCoverage" }
func (t *SecurityCoverageTool) Description() string {
	return "Record security assessment coverage: what was tested, not tested, blocked, not applicable, why, and linked evidence or findings."
}
func (t *SecurityCoverageTool) InputSchema() map[string]interface{} {
	fields := map[string]interface{}{}
	for _, key := range []string{"id", "catalog_id", "area", "target", "status", "tested", "not_tested", "reason", "evidence_id", "finding_id", "notes", "feature"} {
		fields[key] = map[string]interface{}{"type": "string"}
	}
	fields["action"] = map[string]interface{}{"type": "string", "enum": []string{"add", "update", "list", "show", "clear", "seed"}}
	fields["status"] = map[string]interface{}{"type": "string", "enum": []string{"planned", "tested", "not-tested", "blocked", "not-applicable"}}
	fields["target_features"] = map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}
	fields["targets"] = map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}
	return map[string]interface{}{"type": "object", "properties": fields, "required": []string{"action"}}
}
func (t *SecurityCoverageTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "seed":
		seeded := seedSecurityCoverageLocked(input)
		return marshalTool(seeded)
	case "add":
		record := securityCoverageFromInput(input)
		if record.Area == "" && record.CatalogID == "" {
			return &ToolResult{Content: "missing area or catalog_id", IsError: true}, nil
		}
		if record.ID == "" {
			record.ID = "C-" + strings.ToUpper(uuid.New().String()[:8])
		}
		record.UpdatedAt = time.Now().Format(time.RFC3339)
		agentState.Coverage = append(agentState.Coverage, record)
	case "update":
		id := strings.TrimSpace(toString(input["id"]))
		if id == "" {
			return &ToolResult{Content: "missing id", IsError: true}, nil
		}
		for i := range agentState.Coverage {
			if strings.EqualFold(agentState.Coverage[i].ID, id) {
				mergeSecurityCoverage(&agentState.Coverage[i], input)
				agentState.Coverage[i].UpdatedAt = time.Now().Format(time.RFC3339)
				return marshalTool(agentState.Coverage[i])
			}
		}
		return &ToolResult{Content: "security coverage not found: " + id, IsError: true}, nil
	case "show":
		id := strings.TrimSpace(toString(input["id"]))
		for _, coverage := range agentState.Coverage {
			if strings.EqualFold(coverage.ID, id) {
				return marshalTool(coverage)
			}
		}
		return &ToolResult{Content: "security coverage not found: " + id, IsError: true}, nil
	case "clear":
		agentState.Coverage = nil
	case "list", "":
	default:
		return &ToolResult{Content: "unknown security coverage action: " + action, IsError: true}, nil
	}
	return marshalTool(agentState.Coverage)
}

func CurrentMission() MissionState {
	agentState.Lock()
	defer agentState.Unlock()
	return agentState.Mission
}

// CurrentGoal returns a copy of the current goal-mode state.
func CurrentGoal() GoalState {
	agentState.Lock()
	defer agentState.Unlock()
	goal := agentState.Goal
	goal.DoneWhen = append([]string(nil), agentState.Goal.DoneWhen...)
	goal.Progress = append([]string(nil), agentState.Goal.Progress...)
	return goal
}

// SetGoal records the goal objective and (optional) acceptance criteria.
func SetGoal(objective string, doneWhen []string) {
	agentState.Lock()
	defer agentState.Unlock()
	objective = strings.TrimSpace(objective)
	if objective != "" {
		agentState.Goal.Objective = objective
	}
	for _, item := range nonEmptyStrings(doneWhen) {
		agentState.Goal.DoneWhen = appendUniqueLimit(agentState.Goal.DoneWhen, item, 100)
	}
	if agentState.Goal.Status == "" || strings.EqualFold(agentState.Goal.Status, "achieved") || strings.EqualFold(agentState.Goal.Status, "blocked") {
		agentState.Goal.Status = "active"
	}
	agentState.Goal.UpdatedAt = time.Now().Format(time.RFC3339)
}

// ClearGoal resets the goal-mode state.
func ClearGoal() {
	agentState.Lock()
	defer agentState.Unlock()
	agentState.Goal = GoalState{}
}

// GoalStatus returns the normalized status of the current goal.
func GoalStatus() string {
	agentState.Lock()
	defer agentState.Unlock()
	return strings.ToLower(strings.TrimSpace(agentState.Goal.Status))
}

// IncrementGoalIteration counts autonomous goal-mode continuation rounds.
func IncrementGoalIteration() {
	agentState.Lock()
	defer agentState.Unlock()
	agentState.Goal.Iterations++
}

func CurrentScope() []string {
	agentState.Lock()
	defer agentState.Unlock()
	return append([]string(nil), agentState.Scopes...)
}

func CurrentFindings() []FindingRecord {
	agentState.Lock()
	defer agentState.Unlock()
	return append([]FindingRecord(nil), agentState.Findings...)
}

func CurrentEvidence() []EvidenceRecord {
	agentState.Lock()
	defer agentState.Unlock()
	return append([]EvidenceRecord(nil), agentState.Evidence...)
}

func CurrentAssessmentPlan() AssessmentPlanState {
	agentState.Lock()
	defer agentState.Unlock()
	return agentState.AssessmentPlan
}

func CurrentSecurityCoverage() []SecurityCoverageRecord {
	agentState.Lock()
	defer agentState.Unlock()
	return append([]SecurityCoverageRecord(nil), agentState.Coverage...)
}

func CurrentSecurityTests() []SecurityTestRecord {
	agentState.Lock()
	defer agentState.Unlock()
	return append([]SecurityTestRecord(nil), agentState.SecurityTests...)
}

func ExportAgentStateJSON() ([]byte, error) {
	agentState.Lock()
	snapshot := AgentStateSnapshot{
		Mission:        agentState.Mission,
		Goal:           agentState.Goal,
		Scopes:         append([]string(nil), agentState.Scopes...),
		Findings:       append([]FindingRecord(nil), agentState.Findings...),
		Evidence:       append([]EvidenceRecord(nil), agentState.Evidence...),
		AssessmentPlan: agentState.AssessmentPlan,
		Coverage:       append([]SecurityCoverageRecord(nil), agentState.Coverage...),
		SecurityTests:  append([]SecurityTestRecord(nil), agentState.SecurityTests...),
	}
	agentState.Unlock()

	todoState.Lock()
	snapshot.Todos = append([]string(nil), todoState.Items...)
	todoState.Unlock()

	return json.Marshal(snapshot)
}

func RestoreAgentStateJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var snapshot AgentStateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	agentState.Lock()
	agentState.Mission = snapshot.Mission
	agentState.Goal = snapshot.Goal
	agentState.Scopes = append([]string(nil), snapshot.Scopes...)
	agentState.Findings = append([]FindingRecord(nil), snapshot.Findings...)
	agentState.Evidence = append([]EvidenceRecord(nil), snapshot.Evidence...)
	agentState.AssessmentPlan = snapshot.AssessmentPlan
	agentState.Coverage = append([]SecurityCoverageRecord(nil), snapshot.Coverage...)
	agentState.SecurityTests = append([]SecurityTestRecord(nil), snapshot.SecurityTests...)
	agentState.Unlock()

	todoState.Lock()
	todoState.Items = append([]string(nil), snapshot.Todos...)
	todoState.Unlock()
	return nil
}

func AgentStateSummary() string {
	agentState.Lock()
	defer agentState.Unlock()
	var b strings.Builder
	if agentState.Mission.Goal != "" || agentState.Mission.Phase != "" {
		fmt.Fprintf(&b, "Mission: goal=%q phase=%q done_when=%q blocked_by=%q deliverable=%q\n",
			agentState.Mission.Goal, agentState.Mission.Phase, agentState.Mission.DoneWhen, agentState.Mission.BlockedBy, agentState.Mission.Deliverable)
	}
	if agentState.Goal.Objective != "" {
		fmt.Fprintf(&b, "Goal: objective=%q status=%q iterations=%d blocked_by=%q\n",
			agentState.Goal.Objective, agentState.Goal.Status, agentState.Goal.Iterations, agentState.Goal.Blockers)
		if len(agentState.Goal.DoneWhen) > 0 {
			writeSummaryList(&b, "Goal acceptance criteria", agentState.Goal.DoneWhen)
		}
		if len(agentState.Goal.Progress) > 0 {
			progress := agentState.Goal.Progress
			if len(progress) > 6 {
				progress = progress[len(progress)-6:]
			}
			writeSummaryList(&b, "Goal progress (latest)", progress)
		}
	}
	if len(agentState.Scopes) > 0 || len(agentState.Mission.Scope) > 0 {
		writeSummaryList(&b, "Authorized scope", appendUniqueMany(agentState.Scopes, agentState.Mission.Scope, 20))
	}
	if len(agentState.Findings) > 0 {
		var items []string
		for _, f := range agentState.Findings {
			items = append(items, fmt.Sprintf("%s [%s/%s] %s %s", f.ID, f.Severity, f.Status, f.Title, f.Endpoint))
		}
		writeSummaryList(&b, "Findings", items)
	}
	if len(agentState.Evidence) > 0 {
		var items []string
		for _, e := range agentState.Evidence {
			items = append(items, fmt.Sprintf("%s %s %s", e.ID, e.Type, e.Path))
		}
		writeSummaryList(&b, "Evidence files", items)
	}
	if agentState.AssessmentPlan.Objective != "" || agentState.AssessmentPlan.TargetProfile != "" || agentState.AssessmentPlan.TestLevel != "" {
		fmt.Fprintf(&b, "Assessment plan: objective=%q target_profile=%q test_level=%q rules=%q\n",
			agentState.AssessmentPlan.Objective, agentState.AssessmentPlan.TargetProfile, agentState.AssessmentPlan.TestLevel, agentState.AssessmentPlan.Rules)
	}
	if len(agentState.Coverage) > 0 {
		var items []string
		for _, coverage := range agentState.Coverage {
			items = append(items, fmt.Sprintf("%s [%s] %s target=%s reason=%s", coverage.ID, coverage.Status, firstNonEmptyString(coverage.CatalogID, coverage.Area), coverage.Target, coverage.Reason))
		}
		writeSummaryList(&b, "Security coverage", items)
	}
	if len(agentState.SecurityTests) > 0 {
		var items []string
		for _, test := range agentState.SecurityTests {
			items = append(items, fmt.Sprintf("%s [%s] %s outcome=%s", test.ID, test.Vector, test.Title, test.Outcome))
		}
		writeSummaryList(&b, "AI security tests", items)
	}
	return strings.TrimSpace(b.String())
}

func writeSummaryList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
}

func updateMissionLocked(input map[string]interface{}) {
	if v := strings.TrimSpace(toString(input["goal"])); v != "" {
		agentState.Mission.Goal = v
	}
	if raw, ok := input["scope"].([]interface{}); ok {
		agentState.Mission.Scope = nil
		for _, item := range raw {
			if text := strings.TrimSpace(toString(item)); text != "" {
				agentState.Mission.Scope = append(agentState.Mission.Scope, text)
				agentState.Scopes = appendUniqueLimit(agentState.Scopes, text, 200)
			}
		}
	}
	if v := strings.TrimSpace(toString(input["phase"])); v != "" {
		agentState.Mission.Phase = v
	}
	if v := strings.TrimSpace(toString(input["done_when"])); v != "" {
		agentState.Mission.DoneWhen = v
	}
	if v := strings.TrimSpace(toString(input["blocked_by"])); v != "" {
		agentState.Mission.BlockedBy = v
	}
	if v := strings.TrimSpace(toString(input["deliverable"])); v != "" {
		agentState.Mission.Deliverable = v
	}
	agentState.Mission.UpdatedAt = time.Now().Format(time.RFC3339)
}

func updateAssessmentPlanLocked(input map[string]interface{}) {
	if v := strings.TrimSpace(toString(input["objective"])); v != "" {
		agentState.AssessmentPlan.Objective = v
	}
	if v := strings.TrimSpace(toString(input["target_profile"])); v != "" {
		agentState.AssessmentPlan.TargetProfile = v
	}
	if v := normalizeTestLevel(toString(input["test_level"])); v != "" {
		agentState.AssessmentPlan.TestLevel = v
	}
	if values := stringListInput(input["in_scope"]); len(values) > 0 {
		agentState.AssessmentPlan.InScope = appendUniqueMany(agentState.AssessmentPlan.InScope, values, 200)
		agentState.Mission.Scope = appendUniqueMany(agentState.Mission.Scope, values, 200)
		agentState.Scopes = appendUniqueMany(agentState.Scopes, values, 200)
	}
	if values := stringListInput(input["out_of_scope"]); len(values) > 0 {
		agentState.AssessmentPlan.OutOfScope = appendUniqueMany(agentState.AssessmentPlan.OutOfScope, values, 200)
	}
	if values := stringListInput(input["roles"]); len(values) > 0 {
		agentState.AssessmentPlan.Roles = appendUniqueMany(agentState.AssessmentPlan.Roles, values, 100)
	}
	if v := strings.TrimSpace(toString(input["rules"])); v != "" {
		agentState.AssessmentPlan.Rules = v
	}
	agentState.AssessmentPlan.UpdatedAt = time.Now().Format(time.RFC3339)
}

func findingFromInput(input map[string]interface{}) FindingRecord {
	f := FindingRecord{ID: toString(input["id"])}
	mergeFinding(&f, input)
	return f
}

func mergeFinding(f *FindingRecord, input map[string]interface{}) {
	fields := map[string]*string{
		"title":         &f.Title,
		"category":      &f.Category,
		"cwe":           &f.CWE,
		"owasp":         &f.OWASP,
		"cvss":          &f.CVSS,
		"severity":      &f.Severity,
		"confidence":    &f.Confidence,
		"asset":         &f.Asset,
		"endpoint":      &f.Endpoint,
		"parameter":     &f.Parameter,
		"status":        &f.Status,
		"prerequisites": &f.Prerequisites,
		"reproduction":  &f.Reproduction,
		"observed":      &f.Observed,
		"expected":      &f.Expected,
		"evidence":      &f.Evidence,
		"request":       &f.Request,
		"response":      &f.Response,
		"impact":        &f.Impact,
		"remediation":   &f.Remediation,
		"retest":        &f.Retest,
		"notes":         &f.Notes,
	}
	for key, dst := range fields {
		if v := strings.TrimSpace(toString(input[key])); v != "" {
			*dst = v
		}
	}
	normalizeFindingRecord(f)
}

func normalizeFindingRecord(f *FindingRecord) {
	f.Severity = normalizeChoice(f.Severity, map[string]string{
		"critical": "Critical", "crit": "Critical",
		"high": "High", "medium": "Medium", "med": "Medium",
		"low": "Low", "info": "Info", "informational": "Info",
	})
	f.Confidence = normalizeChoice(f.Confidence, map[string]string{
		"high": "High", "medium": "Medium", "med": "Medium", "low": "Low",
	})
	f.Status = normalizeChoice(f.Status, map[string]string{
		"confirmed":      "confirmed",
		"likely":         "likely",
		"false-positive": "false-positive", "false positive": "false-positive", "fp": "false-positive",
		"untested":                 "untested",
		"needs-dynamic-validation": "needs-dynamic-validation", "needs dynamic validation": "needs-dynamic-validation", "needs-validation": "needs-dynamic-validation",
		"duplicate": "duplicate", "accepted-risk": "accepted-risk", "accepted risk": "accepted-risk",
	})
	if f.Status == "" {
		f.Status = "likely"
		if looksLikeHTTPRequest(f.Request) && looksLikeHTTPResponse(f.Response) {
			f.Status = "confirmed"
		}
	}
}

func normalizeChoice(value string, allowed map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if normalized, ok := allowed[strings.ToLower(value)]; ok {
		return normalized
	}
	return value
}

func normalizeTestLevel(value string) string {
	return normalizeChoice(value, map[string]string{
		"passive": "passive", "recon": "passive",
		"low-noise": "low-noise", "low noise": "low-noise", "lownoise": "low-noise", "safe": "low-noise",
		"active":    "active",
		"intrusive": "intrusive", "high-impact": "intrusive", "high impact": "intrusive",
	})
}

func looksLikeHTTPRequest(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	firstLine := strings.ToUpper(strings.TrimSpace(strings.SplitN(value, "\n", 2)[0]))
	for _, method := range []string{"GET ", "POST ", "PUT ", "PATCH ", "DELETE ", "HEAD ", "OPTIONS "} {
		if strings.HasPrefix(firstLine, method) && strings.Contains(firstLine, " HTTP/") {
			return true
		}
	}
	return false
}

func looksLikeHTTPResponse(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	firstLine := strings.ToUpper(strings.TrimSpace(strings.SplitN(value, "\n", 2)[0]))
	return strings.HasPrefix(firstLine, "HTTP/") && strings.Contains(firstLine, " ")
}

func securityCoverageFromInput(input map[string]interface{}) SecurityCoverageRecord {
	record := SecurityCoverageRecord{ID: strings.TrimSpace(toString(input["id"]))}
	mergeSecurityCoverage(&record, input)
	return record
}

func seedSecurityCoverageLocked(input map[string]interface{}) []SecurityCoverageRecord {
	features := append(stringListInput(input["target_features"]), strings.TrimSpace(toString(input["feature"])))
	features = nonEmptyStrings(features)
	if len(features) == 0 && strings.TrimSpace(agentState.AssessmentPlan.TargetProfile) != "" {
		features = strings.FieldsFunc(agentState.AssessmentPlan.TargetProfile, func(r rune) bool {
			return r == ',' || r == ';' || r == '/' || r == '|' || r == ' ' || r == '\t' || r == '\n'
		})
	}
	targets := stringListInput(input["targets"])
	if len(targets) == 0 {
		targets = append([]string(nil), agentState.Scopes...)
	}
	if len(targets) == 0 {
		targets = append([]string(nil), agentState.Mission.Scope...)
	}
	if len(targets) == 0 {
		targets = append([]string(nil), agentState.AssessmentPlan.InScope...)
	}
	target := strings.Join(targets, ", ")
	status := normalizeChoice(toString(input["status"]), map[string]string{
		"planned": "planned", "tested": "tested", "not-tested": "not-tested", "not tested": "not-tested", "blocked": "blocked", "not-applicable": "not-applicable",
	})
	if status == "" {
		status = "planned"
	}
	var seeded []SecurityCoverageRecord
	for _, entry := range securityCatalog {
		entry = enrichSecurityCatalogEntry(entry)
		if len(features) > 0 && !securityCatalogApplies(entry, features) {
			continue
		}
		if coverageExistsLocked(entry.ID, target) {
			continue
		}
		record := SecurityCoverageRecord{
			ID:        "C-" + strings.ToUpper(uuid.New().String()[:8]),
			CatalogID: entry.ID,
			Area:      entry.Name,
			Target:    target,
			Status:    status,
			Reason:    "seeded from SecurityCatalog",
			Notes:     "Evidence required: " + strings.Join(entry.EvidenceRequirements, "; "),
			UpdatedAt: time.Now().Format(time.RFC3339),
		}
		agentState.Coverage = append(agentState.Coverage, record)
		seeded = append(seeded, record)
	}
	return seeded
}

func coverageExistsLocked(catalogID string, target string) bool {
	for _, item := range agentState.Coverage {
		if strings.EqualFold(item.CatalogID, catalogID) && strings.EqualFold(strings.TrimSpace(item.Target), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func mergeSecurityCoverage(record *SecurityCoverageRecord, input map[string]interface{}) {
	fields := map[string]*string{
		"catalog_id":  &record.CatalogID,
		"area":        &record.Area,
		"target":      &record.Target,
		"status":      &record.Status,
		"tested":      &record.Tested,
		"not_tested":  &record.NotTested,
		"reason":      &record.Reason,
		"evidence_id": &record.EvidenceID,
		"finding_id":  &record.FindingID,
		"notes":       &record.Notes,
	}
	for key, dst := range fields {
		if value := strings.TrimSpace(toString(input[key])); value != "" {
			*dst = value
		}
	}
	record.Status = normalizeChoice(record.Status, map[string]string{
		"planned": "planned",
		"tested":  "tested", "done": "tested", "covered": "tested",
		"not-tested": "not-tested", "not tested": "not-tested", "untested": "not-tested",
		"blocked":        "blocked",
		"not-applicable": "not-applicable", "not applicable": "not-applicable", "n/a": "not-applicable", "na": "not-applicable",
	})
	if record.Status == "" {
		record.Status = "planned"
	}
}

func FindingQualityIssues(findings []FindingRecord) []FindingQualityIssue {
	var issues []FindingQualityIssue
	for _, finding := range findings {
		if !strings.EqualFold(strings.TrimSpace(finding.Status), "confirmed") {
			continue
		}
		var missing []string
		if !looksLikeHTTPRequest(finding.Request) || !looksLikeHTTPResponse(finding.Response) {
			missing = append(missing, "Burp Request/Response POC")
		}
		if strings.TrimSpace(finding.Impact) == "" {
			missing = append(missing, "impact")
		}
		if strings.TrimSpace(finding.Remediation) == "" {
			missing = append(missing, "remediation")
		}
		if strings.TrimSpace(finding.Retest) == "" {
			missing = append(missing, "retest method")
		}
		if len(missing) > 0 {
			issues = append(issues, FindingQualityIssue{
				FindingID: finding.ID,
				Title:     finding.Title,
				Status:    finding.Status,
				Missing:   missing,
			})
		}
	}
	return issues
}

func writeEvidenceFile(cfg *config.Config, id string, content string) (string, error) {
	root, err := workspaceRoot(cfg)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, ".cache", "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := strings.ToLower(strings.TrimSpace(id))
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '-'
	}, name)
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.txt", time.Now().Format("20060102-150405"), name))
	return path, os.WriteFile(path, []byte(content), 0644)
}

func marshalTool(value interface{}) (*ToolResult, error) {
	data, _ := json.MarshalIndent(value, "", "  ")
	return &ToolResult{Content: string(data), Output: value}, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendUniqueMany(a []string, b []string, limit int) []string {
	out := append([]string(nil), a...)
	for _, item := range b {
		out = appendUniqueLimit(out, item, limit)
	}
	return out
}

func stringListInput(value interface{}) []string {
	switch raw := value.(type) {
	case []interface{}:
		var out []string
		for _, item := range raw {
			if text := strings.TrimSpace(toString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		var out []string
		for _, item := range raw {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if text := strings.TrimSpace(raw); text != "" {
			return []string{text}
		}
	}
	return nil
}

func appendUniqueLimit(items []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" || (limit > 0 && len(items) >= limit) {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
