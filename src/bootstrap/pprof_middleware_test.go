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

	if lg.msg != "pprof请求处理完成" {
		t.Fatalf("unexpected log message: %s", lg.msg)
	}
	if got := lg.fields["方法"]; got != http.MethodGet {
		t.Fatalf("unexpected method: %v", got)
	}
	if got := lg.fields["路径"]; got != "/debug/pprof/profile" {
		t.Fatalf("unexpected path: %v", got)
	}
	if got := lg.fields["查询串"]; got != "seconds=1" {
		t.Fatalf("unexpected query: %v", got)
	}
	if got := lg.fields["远端地址"]; got != "127.0.0.1:12345" {
		t.Fatalf("unexpected remote addr: %v", got)
	}
	if got := lg.fields["客户端"]; got != "pprof-test" {
		t.Fatalf("unexpected user agent: %v", got)
	}
	if got := lg.fields["状态码"]; got != http.StatusCreated {
		t.Fatalf("unexpected status: %v", got)
	}
	if got := lg.fields["响应字节数"]; got != 2 {
		t.Fatalf("unexpected response bytes: %v", got)
	}
	if got, ok := lg.fields["耗时"].(string); !ok || got == "" {
		t.Fatalf("unexpected duration: %v", lg.fields["耗时"])
	}
}
