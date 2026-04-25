package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchOrCacheSpec_ContentValidityCheck(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		wantErr    string
	}{
		{
			name:       "small HTML error page",
			body:       "<html><body>404 Not Found</body></html>",
			statusCode: 200,
			wantErr:    "does not look like an OpenAPI spec",
		},
		{
			name:       "small status-text error",
			body:       "404: Not Found",
			statusCode: 200,
			wantErr:    "does not look like an OpenAPI spec",
		},
		{
			name:       "valid large spec passes",
			body:       "{\"openapi\":\"3.0.0\",\"info\":{\"title\":\"Test\",\"version\":\"1.0\"},\"paths\":{}}" + string(make([]byte, 300)),
			statusCode: 200,
			wantErr:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := fetchOrCacheSpec(srv.URL, true, true)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || searchString(s, sub))
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
