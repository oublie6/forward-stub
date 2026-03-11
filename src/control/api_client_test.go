package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchBusinessConfigRejectsSystemFields(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":1,"receivers":{},"senders":{},"pipelines":{},"tasks":{},"logging":{"level":"debug"}}`))
	}))
	defer ts.Close()

	cli := NewConfigAPIClient(ts.URL, 2)
	if _, err := cli.FetchBusinessConfig(context.Background()); err == nil {
		t.Fatalf("expected decode error for unknown/system field")
	}
}
