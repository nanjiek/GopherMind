package surface

import (
	"fmt"
	"time"

	"gophermind/internal/core/model"
)

const (
	ConversationAgent = "conversation"
	CurrentVersion    = 1
)

type Key struct {
	TenantID  string
	UserID    string
	SessionID string
	Agent     string
	Version   int
}

func ConversationKey(userID string, sessionID string) Key {
	return Key{
		TenantID:  "default",
		UserID:    userID,
		SessionID: sessionID,
		Agent:     ConversationAgent,
		Version:   CurrentVersion,
	}
}

func (k Key) RedisKey() string {
	return fmt.Sprintf("surface:%s:%s:%s:%s:%d", k.TenantID, k.UserID, k.SessionID, k.Agent, k.Version)
}

type Surface struct {
	Key               Key             `json:"key"`
	SourceSeq         int64           `json:"source_seq"`
	ProjectionVersion int             `json:"projection_version"`
	ExpiresAt         time.Time       `json:"expires_at"`
	Messages          []model.Message `json:"messages"`
}

func (s Surface) FreshFor(sourceSeq int64, now time.Time) bool {
	return s.Key.Version == CurrentVersion &&
		s.ProjectionVersion == CurrentVersion &&
		s.SourceSeq == sourceSeq &&
		now.Before(s.ExpiresAt)
}
