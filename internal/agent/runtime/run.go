package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalidRun         = errors.New("runtime run is invalid")
	ErrInvalidTransition  = errors.New("runtime run transition is invalid")
	ErrTerminalRun        = errors.New("runtime run is terminal")
	ErrInvalidAction      = errors.New("runtime action is invalid")
	ErrInvalidObservation = errors.New("runtime observation is invalid")
	ErrMaxStepsExceeded   = errors.New("runtime run maximum steps exceeded")
	ErrRevisionConflict   = errors.New("runtime run revision conflict")
	ErrNoProgress         = errors.New("runtime run made no progress")
	ErrDeadlineExceeded   = errors.New("runtime run deadline exceeded")
)

// FailureKind classifies a terminal runtime failure without exposing model
// reasoning. Retry policy remains a later executor concern.
type FailureKind string

const (
	FailureTransient  FailureKind = "transient"
	FailureTimeout    FailureKind = "timeout"
	FailureValidation FailureKind = "validation"
	FailureContext    FailureKind = "context"
	FailureDependency FailureKind = "dependency"
	FailureConflict   FailureKind = "conflict"
	FailurePolicy     FailureKind = "policy"
	FailureNoProgress FailureKind = "no_progress"
	FailurePermanent  FailureKind = "permanent"
)

// RunStatus is the explicit lifecycle state of a single agent run.
type RunStatus string

const (
	RunCreated        RunStatus = "created"
	RunLoadingContext RunStatus = "loading_context"
	RunRouting        RunStatus = "routing"
	RunRunning        RunStatus = "running"
	RunWaitingTool    RunStatus = "waiting_tool"
	RunWaitingAgent   RunStatus = "waiting_agent"
	RunWaitingUser    RunStatus = "waiting_user"
	RunWaitingHuman   RunStatus = "waiting_human"
	RunValidating     RunStatus = "validating"
	RunCompleted      RunStatus = "completed"
	RunRetryScheduled RunStatus = "retry_scheduled"
	RunFailed         RunStatus = "failed"
	RunCancelled      RunStatus = "cancelled"
	RunExpired        RunStatus = "expired"
)

// ActionType is an allowed structured result of agent reasoning. It does not
// execute a tool or skill; execution and policy checks are later runtime work.
type ActionType string

const (
	ActionAskUser       ActionType = "ask_user"
	ActionCallSkill     ActionType = "call_skill"
	ActionCallTool      ActionType = "call_tool"
	ActionDelegateTask  ActionType = "delegate_task"
	ActionReturnResult  ActionType = "return_result"
	ActionEscalateHuman ActionType = "escalate_human"
)

// RunSpec is the immutable metadata used when a run is created.
type RunSpec struct {
	RunID           string
	Scope           Metadata
	WorkflowID      string
	WorkflowVersion string
	MaxSteps        int
	MaxNoProgress   int
	Deadline        time.Time
	TokenBudget     int64
	CostBudget      float64
}

// Action is a structured request produced while a run is running.
// Arguments are JSON data rather than model-hidden reasoning.
type Action struct {
	ActionID       string
	RunID          string
	AgentID        string
	Step           int
	Type           ActionType
	ReasonCode     string
	TargetID       string
	Arguments      json.RawMessage
	InputSchema    string
	ExpectedOutput string
	IdempotencyKey string
	CreatedAt      time.Time
}

// Observation records the result of one awaited action. Output is structured
// data or a reference encoded as JSON; persistence is deliberately outside
// this in-memory state-machine node.
type Observation struct {
	ObservationID string
	RunID         string
	ActionID      string
	Step          int
	Output        json.RawMessage
	ErrorCode     string
	CreatedAt     time.Time
}

// RunSnapshot is a copy of the current in-memory state. It intentionally has
// no revision or persistence fields; those arrive in a separate P2 node.
type RunSnapshot struct {
	RunSpec
	Status       RunStatus
	CurrentNode  string
	StepCount    int
	Revision     int64
	ErrorCode    string
	FailureKind  FailureKind
	Actions      []Action
	Observations []Observation
}

// Run owns the synchronized in-memory Run/Action/Observation protocol.
type Run struct {
	mu sync.Mutex

	spec         RunSpec
	status       RunStatus
	currentNode  string
	stepCount    int
	revision     int64
	errorCode    string
	failureKind  FailureKind
	actions      []Action
	observations []Observation
	pending      Action
	hasPending   bool
	lastProgress string
	noProgress   int
}

// NewRun creates an in-memory run in the created state.
func NewRun(spec RunSpec) (*Run, error) {
	if spec.RunID == "" || spec.WorkflowID == "" || spec.WorkflowVersion == "" || spec.MaxSteps <= 0 || spec.MaxNoProgress < 0 {
		return nil, fmt.Errorf("%w: run ID, workflow ID, workflow version, and positive max steps are required", ErrInvalidRun)
	}
	if spec.TokenBudget < 0 || spec.CostBudget < 0 {
		return nil, fmt.Errorf("%w: budgets cannot be negative", ErrInvalidRun)
	}
	return &Run{spec: spec, status: RunCreated, revision: 1}, nil
}

// Transition moves the run through core lifecycle edges. SubmitAction, Observe,
// and Complete own the action-derived waiting, validating, and completed edges.
// Fail, cancel, expire, and retry scheduling are available from every
// non-terminal state.
func (r *Run) Transition(next RunStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transitionLocked(next)
}

// TransitionCAS performs a transition only if expectedRevision is current.
func (r *Run) TransitionCAS(expectedRevision int64, next RunStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.requireRevisionLocked(expectedRevision); err != nil {
		return err
	}
	return r.transitionLocked(next)
}

func (r *Run) transitionLocked(next RunStatus) error {
	if r.status.terminal() {
		return fmt.Errorf("%w: %s", ErrTerminalRun, r.status)
	}
	if !canTransition(r.status, next) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, r.status, next)
	}
	r.status = next
	if next == RunRetryScheduled || next == RunFailed || next == RunCancelled || next == RunExpired {
		r.hasPending = false
	}
	r.revision++
	return nil
}

// SetCurrentNode records the active workflow node while the run is live.
func (r *Run) SetCurrentNode(node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setCurrentNodeLocked(node)
}

// SetCurrentNodeCAS updates the active node only if expectedRevision is current.
func (r *Run) SetCurrentNodeCAS(expectedRevision int64, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.requireRevisionLocked(expectedRevision); err != nil {
		return err
	}
	return r.setCurrentNodeLocked(node)
}

func (r *Run) setCurrentNodeLocked(node string) error {
	if r.status.terminal() {
		return fmt.Errorf("%w: %s", ErrTerminalRun, r.status)
	}
	r.currentNode = node
	r.revision++
	return nil
}

// SubmitAction records one allowed action and moves the run to the matching
// wait/validation state. It never executes that action.
func (r *Run) SubmitAction(action Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submitActionLocked(action)
}

// SubmitActionCAS records an action only if expectedRevision is current.
func (r *Run) SubmitActionCAS(expectedRevision int64, action Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.requireRevisionLocked(expectedRevision); err != nil {
		return err
	}
	return r.submitActionLocked(action)
}

func (r *Run) submitActionLocked(action Action) error {
	if r.status.terminal() {
		return fmt.Errorf("%w: %s", ErrTerminalRun, r.status)
	}
	if r.status != RunRunning {
		return fmt.Errorf("%w: actions require %s, got %s", ErrInvalidAction, RunRunning, r.status)
	}
	if r.stepCount >= r.spec.MaxSteps {
		return ErrMaxStepsExceeded
	}
	if !r.spec.Deadline.IsZero() && !time.Now().Before(r.spec.Deadline) {
		r.status = RunExpired
		r.hasPending = false
		r.revision++
		return ErrDeadlineExceeded
	}
	if err := validateAction(r.spec.RunID, r.stepCount+1, action); err != nil {
		return err
	}
	if r.spec.MaxNoProgress > 0 {
		fingerprint := actionFingerprint(action)
		if fingerprint == r.lastProgress {
			r.noProgress++
		} else {
			r.lastProgress = fingerprint
			r.noProgress = 0
		}
		if r.noProgress >= r.spec.MaxNoProgress {
			r.status = RunFailed
			r.errorCode = string(FailureNoProgress)
			r.failureKind = FailureNoProgress
			r.hasPending = false
			r.revision++
			return ErrNoProgress
		}
	}

	action.Arguments = cloneJSON(action.Arguments)
	r.stepCount++
	r.actions = append(r.actions, action)
	r.pending = action
	r.hasPending = true
	r.status = actionWaitStatus(action.Type)
	r.revision++
	return nil
}

// Observe completes the currently awaited non-result action and resumes the
// run. A return_result action instead moves to validating and must be finished
// by Complete.
func (r *Run) Observe(observation Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.observeLocked(observation)
}

// ObserveCAS records an observation only if expectedRevision is current.
func (r *Run) ObserveCAS(expectedRevision int64, observation Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.requireRevisionLocked(expectedRevision); err != nil {
		return err
	}
	return r.observeLocked(observation)
}

func (r *Run) observeLocked(observation Observation) error {
	if r.status.terminal() {
		return fmt.Errorf("%w: %s", ErrTerminalRun, r.status)
	}
	if !r.hasPending || r.pending.Type == ActionReturnResult {
		return fmt.Errorf("%w: no observable pending action", ErrInvalidObservation)
	}
	if !r.status.waiting() {
		return fmt.Errorf("%w: observations require a waiting run, got %s", ErrInvalidObservation, r.status)
	}
	if err := validateObservation(r.spec.RunID, r.pending, observation); err != nil {
		return err
	}

	observation.Output = cloneJSON(observation.Output)
	r.observations = append(r.observations, observation)
	r.hasPending = false
	r.status = RunRunning
	r.revision++
	return nil
}

// Complete commits a validated return_result action as a completed run.
func (r *Run) Complete() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeLocked()
}

// CompleteCAS completes a result only if expectedRevision is current.
func (r *Run) CompleteCAS(expectedRevision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.requireRevisionLocked(expectedRevision); err != nil {
		return err
	}
	return r.completeLocked()
}

func (r *Run) completeLocked() error {
	if r.status.terminal() {
		return fmt.Errorf("%w: %s", ErrTerminalRun, r.status)
	}
	if r.status != RunValidating || !r.hasPending || r.pending.Type != ActionReturnResult {
		return fmt.Errorf("%w: completion requires a validating return_result action", ErrInvalidTransition)
	}
	r.hasPending = false
	r.status = RunCompleted
	r.revision++
	return nil
}

// Fail records a stable error code and terminates a non-terminal run.
func (r *Run) Fail(errorCode string) error {
	return r.FailWith(FailurePermanent, errorCode)
}

// FailWith records a classified terminal failure.
func (r *Run) FailWith(kind FailureKind, errorCode string) error {
	if errorCode == "" {
		return fmt.Errorf("%w: failure requires an error code", ErrInvalidRun)
	}
	if !kind.valid() {
		return fmt.Errorf("%w: unknown failure kind", ErrInvalidRun)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.terminal() {
		return fmt.Errorf("%w: %s", ErrTerminalRun, r.status)
	}
	r.errorCode = errorCode
	r.failureKind = kind
	r.hasPending = false
	r.status = RunFailed
	r.revision++
	return nil
}

// Snapshot returns a deep-enough copy for callers to inspect without mutating
// the machine's action or observation JSON buffers.
func (r *Run) Snapshot() RunSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return RunSnapshot{
		RunSpec:      r.spec,
		Status:       r.status,
		CurrentNode:  r.currentNode,
		StepCount:    r.stepCount,
		Revision:     r.revision,
		ErrorCode:    r.errorCode,
		FailureKind:  r.failureKind,
		Actions:      cloneActions(r.actions),
		Observations: cloneObservations(r.observations),
	}
}

func (r *Run) requireRevisionLocked(expected int64) error {
	if expected != r.revision {
		return fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expected, r.revision)
	}
	return nil
}

func validateAction(runID string, expectedStep int, action Action) error {
	if action.ActionID == "" || action.RunID != runID || action.Step != expectedStep || !action.Type.valid() {
		return fmt.Errorf("%w: action ID, matching run ID, next step, and allowed type are required", ErrInvalidAction)
	}
	if len(action.Arguments) > 0 && !json.Valid(action.Arguments) {
		return fmt.Errorf("%w: arguments must be valid JSON", ErrInvalidAction)
	}
	return nil
}

func validateObservation(runID string, action Action, observation Observation) error {
	if observation.ObservationID == "" || observation.RunID != runID || observation.ActionID != action.ActionID || observation.Step != action.Step {
		return fmt.Errorf("%w: observation must identify the current action and step", ErrInvalidObservation)
	}
	if len(observation.Output) > 0 && !json.Valid(observation.Output) {
		return fmt.Errorf("%w: output must be valid JSON", ErrInvalidObservation)
	}
	return nil
}

func canTransition(from, to RunStatus) bool {
	if to == RunRetryScheduled || to == RunFailed || to == RunCancelled || to == RunExpired {
		return true
	}
	switch from {
	case RunCreated:
		return to == RunLoadingContext
	case RunLoadingContext:
		return to == RunRouting
	case RunRouting:
		return to == RunRunning
	case RunRetryScheduled:
		return to == RunLoadingContext
	default:
		return false
	}
}

func (status RunStatus) terminal() bool {
	return status == RunCompleted || status == RunFailed || status == RunCancelled || status == RunExpired
}

func (status RunStatus) waiting() bool {
	return status == RunWaitingTool || status == RunWaitingAgent || status == RunWaitingUser || status == RunWaitingHuman
}

func (actionType ActionType) valid() bool {
	switch actionType {
	case ActionAskUser, ActionCallSkill, ActionCallTool, ActionDelegateTask, ActionReturnResult, ActionEscalateHuman:
		return true
	default:
		return false
	}
}

func (kind FailureKind) valid() bool {
	switch kind {
	case FailureTransient, FailureTimeout, FailureValidation, FailureContext, FailureDependency, FailureConflict, FailurePolicy, FailureNoProgress, FailurePermanent:
		return true
	default:
		return false
	}
}

func actionFingerprint(action Action) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(action.Type))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(action.TargetID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(action.Arguments)
	return string(hash.Sum(nil))
}

func actionWaitStatus(actionType ActionType) RunStatus {
	switch actionType {
	case ActionAskUser:
		return RunWaitingUser
	case ActionCallSkill, ActionCallTool:
		return RunWaitingTool
	case ActionDelegateTask:
		return RunWaitingAgent
	case ActionEscalateHuman:
		return RunWaitingHuman
	case ActionReturnResult:
		return RunValidating
	default:
		panic("validated action type has no wait status")
	}
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func cloneActions(actions []Action) []Action {
	cloned := make([]Action, len(actions))
	copy(cloned, actions)
	for index := range cloned {
		cloned[index].Arguments = cloneJSON(cloned[index].Arguments)
	}
	return cloned
}

func cloneObservations(observations []Observation) []Observation {
	cloned := make([]Observation, len(observations))
	copy(cloned, observations)
	for index := range cloned {
		cloned[index].Output = cloneJSON(cloned[index].Output)
	}
	return cloned
}
