package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gophermind/internal/core/model"
)

func TestBaselineWindowRebuiltAfterCacheLoss(t *testing.T) {
	repo := newFakeRepo()
	for i := 0; i < 9; i++ {
		repo.msgs["s1"] = append(repo.msgs["s1"], model.Message{Role: "user", Content: fmt.Sprint(i)})
	}
	svc := NewSessionService(repo, newFakeCache(), nil)
	window, err := svc.LoadWindow(context.Background(), "u1", "s1")
	require.NoError(t, err)
	require.Len(t, window, 6)
	require.Equal(t, "3", window[0].Content)
	require.Equal(t, "8", window[5].Content)
	require.Len(t, repo.msgs["s1"], 9, "context truncation must not delete persisted messages")
}

type baselineStreamRouter struct{ fakeRouter }

func (*baselineStreamRouter) GenerateStreamWithFallback(ctx context.Context, _ string, _ string, emit func(string) error) (string, model.Usage, error) {
	if err := ctx.Err(); err != nil {
		return "", model.Usage{}, err
	}
	if err := emit("hello"); err != nil {
		return "", model.Usage{}, err
	}
	return "hello", model.Usage{Provider: "fake"}, nil
}

func TestBaselineStreamDelivery(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(fmt.Sprint(disconnect), func(t *testing.T) {
			repo, cache := newFakeRepo(), newFakeCache()
			sessions := NewSessionService(repo, cache, nil)
			svc := NewStreamService(repo, sessions, &baselineStreamRouter{}, &fakeRAG{}, cache, nil, nil, nil, nil)
			closed := errors.New("client disconnected")
			var chunks []string
			out, err := svc.Stream(context.Background(), model.QueryInput{UserID: "u1", Question: "hello"}, func(chunk string) error {
				chunks = append(chunks, chunk)
				if disconnect {
					return closed
				}
				return nil
			})
			require.Equal(t, []string{"hello"}, chunks)
			if disconnect {
				require.ErrorIs(t, err, closed)
				require.Len(t, repo.msgs["s1"], 1, "failed output must not be persisted as a completed answer")
			} else {
				require.NoError(t, err)
				require.Equal(t, "hello", out.Answer)
				require.Len(t, repo.msgs["s1"], 2)
			}
		})
	}
}
