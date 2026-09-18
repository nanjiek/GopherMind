package projection

import (
	"context"
	"time"
)

type Checkpoint struct {
	ProjectionName string
	Shard          int
	LastEventID    *string
	LastRecordedAt *time.Time
	UpdatedAt      time.Time
}

type Store interface {
	Load(ctx context.Context, projectionName string, shard int) (Checkpoint, bool, error)
	Save(ctx context.Context, checkpoint Checkpoint) error
}
