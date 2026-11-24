package auth_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
)

func TestSupabaseAdminDeleteUser(t *testing.T) {
	const key = "service-role-key-for-tests"
	for _, tc := range []struct {
		name   string
		status int
		ok     bool
	}{
		{"deleted", http.StatusOK, true},
		{"already gone", http.StatusNotFound, true},
		{"forbidden", http.StatusForbidden, false},
		{"server error", http.StatusInternalServerError, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath, gotAPIKey, gotAuth, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				gotAPIKey, gotAuth = r.Header.Get("apikey"), r.Header.Get("Authorization")
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"msg":"secret detail ` + key + `"}`))
			}))
			defer srv.Close()
			err := auth.NewSupabaseAdmin(srv.URL+"/", key, nil).DeleteUser(context.Background(), strings.ToUpper(userID))
			if gotMethod != http.MethodDelete || gotPath != "/auth/v1/admin/users/"+userID {
				t.Fatalf("request %s %s", gotMethod, gotPath)
			}
			if gotAPIKey != key || gotAuth != "Bearer "+key {
				t.Fatalf("headers apikey=%q auth=%q", gotAPIKey, gotAuth)
			}
			if !strings.Contains(gotBody, `"should_soft_delete":false`) {
				t.Fatalf("body %q", gotBody)
			}
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && (strings.Contains(err.Error(), key) || strings.Contains(err.Error(), srv.URL)) {
				t.Fatalf("error leaks key or URL: %v", err)
			}
		})
	}
}

func TestSupabaseAdminErrors(t *testing.T) {
	a := auth.NewSupabaseAdmin("http://127.0.0.1:1", "k", nil)
	if err := a.DeleteUser(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("invalid id must fail")
	}
	err := a.DeleteUser(context.Background(), userID)
	if err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("connection error = %v (must not contain URL)", err)
	}
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer slow.Close()
	defer close(release)
	c := &http.Client{Timeout: 50 * time.Millisecond}
	if err := auth.NewSupabaseAdmin(slow.URL, "k", c).DeleteUser(context.Background(), userID); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("timeout err = %v", err)
	}
}
