package ctxlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
)

func TestHandler_Handle(t *testing.T) {
	tests := []struct {
		name          string
		ctx           context.Context
		wantRequestID string
		wantInitiator string
	}{
		{
			name:          "success - request id and initiator are added",
			ctx:           ctxlog.WithInitiator(ctxlog.WithRequestID(context.Background(), "req-1"), "a@b.c"),
			wantRequestID: "req-1",
			wantInitiator: "a@b.c",
		},
		{
			name:          "success - only request id",
			ctx:           ctxlog.WithRequestID(context.Background(), "req-2"),
			wantRequestID: "req-2",
		},
		{
			name: "success - empty context adds nothing",
			ctx:  context.Background(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(ctxlog.NewHandler(slog.NewJSONHandler(&buf, nil))).With(slog.String("service", "x"))

			log.InfoContext(tt.ctx, "hello")

			var rec map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
			assert.Equal(t, "x", rec["service"])
			if tt.wantRequestID == "" {
				assert.NotContains(t, rec, "request_id")
			} else {
				assert.Equal(t, tt.wantRequestID, rec["request_id"])
			}
			if tt.wantInitiator == "" {
				assert.NotContains(t, rec, "initiator")
			} else {
				assert.Equal(t, tt.wantInitiator, rec["initiator"])
			}
		})
	}
}
