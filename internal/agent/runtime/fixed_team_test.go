package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestFixedTeamCoordinatorExecutesClosedDAGWithPrivateInputs(t *testing.T) {
	tasks, mailbox := &memoryTeamTasks{}, &memoryTeamMailbox{}
	inputs := map[string]json.RawMessage{}
	agent := func(name, output string) TeamAgent {
		return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
			inputs[name] = cloneJSON(input)
			return json.RawMessage(output), nil
		}
	}
	coordinator := &FixedTeamCoordinator{Tasks: tasks, Mailbox: mailbox, Team: FixedTeam{
		Intake: agent("intake", `{"intake":"done"}`), Triage: agent("triage", `{"triage":"low"}`), Evidence: agent("evidence", `{"evidence":"found"}`), Safety: agent("safety", `{"approved":true}`), Response: agent("response", `{"answer":"ok"}`),
	}}
	response, err := coordinator.Start(context.Background(), FixedTeamSpec{RunID: "run-a", Scope: Metadata{TenantID: "tenant-a", UserID: "user-a"}, Deadline: time.Now().Add(time.Hour)}, json.RawMessage(`{"question":"q"}`))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got, want := string(response.Data), `{"answer":"ok"}`; got != want {
		t.Fatalf("response = %s, want %s", got, want)
	}
	if got, want := string(inputs["evidence"]), `{"intake":{"intake":"done"},"triage":{"triage":"low"}}`; got != want {
		t.Fatalf("evidence input = %s, want %s", got, want)
	}
	if got, want := string(inputs["safety"]), `{"evidence":{"evidence":"found"}}`; got != want {
		t.Fatalf("safety input = %s, want %s", got, want)
	}
	if got, want := string(inputs["response"]), `{"safety":{"approved":true}}`; got != want {
		t.Fatalf("response input = %s, want %s", got, want)
	}
	for _, task := range tasks.dag.Tasks {
		if task.Status != TaskSucceeded {
			t.Fatalf("task %s status = %s", task.Type, task.Status)
		}
	}
	for _, message := range mailbox.messages {
		if message.Status != MailboxDelivered {
			t.Fatalf("message %s status = %s", message.TaskID, message.Status)
		}
	}
}

func TestFixedTeamCoordinatorPlansOnlyTrustedStaticPaths(t *testing.T) {
	coordinator := &FixedTeamCoordinator{}
	scope := Metadata{TenantID: "tenant-a", UserID: "user-a"}
	for _, test := range []struct {
		path  FixedTeamPath
		types []string
	}{
		{FixedTeamPathSimple, []string{"evidence", "safety", "response"}},
		{FixedTeamPathStandard, []string{"intake", "triage", "evidence", "safety", "response"}},
		{FixedTeamPathHuman, []string{"triage"}},
	} {
		dag, err := coordinator.plan(FixedTeamSpec{RunID: "run-a", Scope: scope, Deadline: time.Now().Add(time.Hour), Path: test.path}, json.RawMessage(`{"question":"q"}`))
		if err != nil {
			t.Fatalf("plan(%s) error = %v", test.path, err)
		}
		if len(dag.Tasks) != len(test.types) {
			t.Fatalf("plan(%s) tasks = %d", test.path, len(dag.Tasks))
		}
		for index, task := range dag.Tasks {
			if task.Type != test.types[index] {
				t.Fatalf("plan(%s) task %d = %s, want %s", test.path, index, task.Type, test.types[index])
			}
		}
	}
	if _, err := coordinator.plan(FixedTeamSpec{RunID: "run-a", Scope: scope, Deadline: time.Now().Add(time.Hour), Path: "invented"}, json.RawMessage(`{"question":"q"}`)); err == nil {
		t.Fatal("plan(invented) error = nil")
	}
}

type memoryTeamTasks struct{ dag TaskDAG }

func (s *memoryTeamTasks) Create(_ context.Context, dag TaskDAG) (TaskDAG, error) {
	value, err := NewTaskDAG(dag)
	if err != nil {
		return TaskDAG{}, err
	}
	s.dag = value
	return cloneTaskDAG(value), nil
}
func (s *memoryTeamTasks) Load(context.Context, Metadata, string) (TaskDAG, error) {
	return cloneTaskDAG(s.dag), nil
}
func (s *memoryTeamTasks) Claim(_ context.Context, _ Metadata, id string, revision int64, worker string, _ time.Duration) (TaskLease, error) {
	for i := range s.dag.Tasks {
		task := &s.dag.Tasks[i]
		if task.TaskID == id {
			if task.Status != TaskReady {
				return TaskLease{}, ErrTaskNotReady
			}
			if task.Revision != revision {
				return TaskLease{}, ErrTaskDAGConflict
			}
			task.Status = TaskRunning
			task.Revision++
			return TaskLease{Task: *task, WorkerID: worker, FencingToken: task.Revision, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}
	}
	return TaskLease{}, ErrTaskDAGNotFound
}
func (s *memoryTeamTasks) Complete(_ context.Context, _ Metadata, id string, revision, _ int64, completion TaskCompletion) (AgentTask, error) {
	for i := range s.dag.Tasks {
		task := &s.dag.Tasks[i]
		if task.TaskID == id {
			if task.Status != TaskRunning || task.Revision != revision {
				return AgentTask{}, ErrTaskDAGConflict
			}
			task.Status, task.Output = completion.Status, cloneJSON(completion.Output)
			task.Revision++
			if completion.Status == TaskSucceeded {
				s.release()
			}
			return *task, nil
		}
	}
	return AgentTask{}, ErrTaskDAGNotFound
}
func (s *memoryTeamTasks) release() {
	for i := range s.dag.Tasks {
		task := &s.dag.Tasks[i]
		if task.Status != TaskBlocked {
			continue
		}
		ready := true
		for _, id := range task.BlockedBy {
			dependency, ok := fixedTeamTask(s.dag, id)
			if !ok || dependency.Status != TaskSucceeded {
				ready = false
				break
			}
		}
		if ready {
			task.Status = TaskReady
			task.Revision++
		}
	}
}
func (s *memoryTeamTasks) RecoverExpired(context.Context, Metadata, string, time.Time) (int, error) {
	return 0, nil
}
func (s *memoryTeamTasks) ExpireBlocked(context.Context, Metadata, string, time.Time) (int, error) {
	return 0, nil
}

type memoryTeamMailbox struct{ messages []AgentMessage }

func (s *memoryTeamMailbox) Enqueue(_ context.Context, message AgentMessage) (AgentMessage, error) {
	for _, existing := range s.messages {
		if existing.IdempotencyKey == message.IdempotencyKey {
			return AgentMessage{}, ErrMailboxConflict
		}
	}
	message.Status = MailboxQueued
	s.messages = append(s.messages, message)
	return cloneMailboxMessage(message), nil
}
func (s *memoryTeamMailbox) Claim(_ context.Context, _ Metadata, _ string, target, worker string, _ time.Duration) (MailboxDelivery, error) {
	for i := range s.messages {
		message := &s.messages[i]
		if message.TargetAgentID == target && message.Status == MailboxQueued {
			message.Status = MailboxClaimed
			message.Revision++
			return MailboxDelivery{Message: cloneMailboxMessage(*message), WorkerID: worker, FencingToken: message.Revision, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}
	}
	return MailboxDelivery{}, ErrMailboxNotFound
}
func (s *memoryTeamMailbox) Acknowledge(_ context.Context, _ Metadata, id string, revision, _ int64) (AgentMessage, error) {
	for i := range s.messages {
		message := &s.messages[i]
		if message.MessageID == id {
			if message.Status != MailboxClaimed || message.Revision != revision {
				return AgentMessage{}, ErrMailboxConflict
			}
			message.Status = MailboxDelivered
			message.Revision++
			return cloneMailboxMessage(*message), nil
		}
	}
	return AgentMessage{}, ErrMailboxNotFound
}
func (s *memoryTeamMailbox) RecoverExpired(context.Context, Metadata, string, time.Time) (int, error) {
	return 0, nil
}
