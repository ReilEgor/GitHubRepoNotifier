package probe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantKey    string
		wantValue  string
	}{
		{
			name:       "success - health reports ok",
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
			wantKey:    "status",
			wantValue:  "ok",
		},
		{
			name:       "success - version reports the build commit",
			method:     http.MethodGet,
			path:       "/version",
			wantStatus: http.StatusOK,
			wantKey:    "version",
			wantValue:  "abc123",
		},
		{
			name:       "error - unknown path",
			method:     http.MethodGet,
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "error - wrong method",
			method:     http.MethodPost,
			path:       "/health",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	handler := NewHandler("abc123")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantKey == "" {
				return
			}

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body[tt.wantKey] != tt.wantValue {
				t.Errorf("%s = %q, want %q", tt.wantKey, body[tt.wantKey], tt.wantValue)
			}
		})
	}
}
