package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type captureInfowLogger struct {
	msg    string
	fields map[string]interface{}
}

func (l *captureInfowLogger) Infow(msg string, kv ...interface{}) {
	l.msg = msg
	l.fields = map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		l.fields[k] = kv[i+1]
	}
}

func TestWithPprofRequestLogRecordsRequestDetails(t *testing.T) {
	lg := &captureInfowLogger{}
	h := withPprofRequestLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}), lg)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile?seconds=1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("User-Agent", "pprof-test")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if lg.msg != "pprof request completed" {
		t.Fatalf("unexpected log message: %s", lg.msg)
	}
	if got := lg.fields["method"]; got != http.MethodGet {
		t.Fatalf("unexpected method: %v", got)
	}
	if got := lg.fields["path"]; got != "/debug/pprof/profile" {
		t.Fatalf("unexpected path: %v", got)
	}
	if got := lg.fields["query"]; got != "seconds=1" {
		t.Fatalf("unexpected query: %v", got)
	}
	if got := lg.fields["remote_addr"]; got != "127.0.0.1:12345" {
		t.Fatalf("unexpected remote addr: %v", got)
	}
	if got := lg.fields["user_agent"]; got != "pprof-test" {
		t.Fatalf("unexpected user agent: %v", got)
	}
	if got := lg.fields["status"]; got != http.StatusCreated {
		t.Fatalf("unexpected status: %v", got)
	}
	if got := lg.fields["response_bytes"]; got != 2 {
		t.Fatalf("unexpected response bytes: %v", got)
	}
	if got, ok := lg.fields["duration"].(string); !ok || got == "" {
		t.Fatalf("unexpected duration: %v", lg.fields["duration"])
	}
}
