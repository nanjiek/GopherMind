package runtime

import (
	"errors"
	"testing"
)

func TestValidateMailboxMessage(t *testing.T) {
	message := testMailboxMessage()
	if err := ValidateMailboxMessage(message); err != nil {
		t.Fatalf("ValidateMailboxMessage() error = %v", err)
	}
	invalid := []AgentMessage{
		func() AgentMessage { value := message; value.Scope.UserID = ""; return value }(),
		func() AgentMessage { value := message; value.Revision = 2; return value }(),
		func() AgentMessage { value := message; value.Payload = []byte(`[]`); return value }(),
		func() AgentMessage { value := message; value.Status = MailboxDelivered; return value }(),
	}
	for _, value := range invalid {
		if err := ValidateMailboxMessage(value); !errors.Is(err, ErrInvalidMailboxMessage) {
			t.Fatalf("ValidateMailboxMessage(%#v) error = %v", value, err)
		}
	}
}

func TestCloneMailboxMessageCopiesPayload(t *testing.T) {
	message := testMailboxMessage()
	cloned := cloneMailboxMessage(message)
	copy(message.Payload, []byte(`{"changed":true}`))
	if got, want := string(cloned.Payload), `{"handoff":"evidence"}`; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func testMailboxMessage() AgentMessage {
	return AgentMessage{
		MessageID: "message-a", RunID: "run-a", Scope: Metadata{TenantID: "tenant-a", UserID: "user-a"},
		SenderAgentID: "intake", TargetAgentID: "evidence", IdempotencyKey: "message-key", Revision: 1,
		Payload: []byte(`{"handoff":"evidence"}`),
	}
}
