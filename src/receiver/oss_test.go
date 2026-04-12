package receiver

import (
	"testing"

	"forward-stub/src/config"
)

func TestOSSReceiverSeenTracksObjectSignature(t *testing.T) {
	r := &OSSReceiver{seen: make(map[string]string)}
	if r.isSeen("a.txt", "sig-1") {
		t.Fatalf("new object should not be seen")
	}
	r.markSeen("a.txt", "sig-1")
	if !r.isSeen("a.txt", "sig-1") {
		t.Fatalf("same signature should be seen")
	}
	if r.isSeen("a.txt", "sig-2") {
		t.Fatalf("changed signature should be processed again")
	}
}

func TestOSSReceiverSeenEvictsOldestWhenBounded(t *testing.T) {
	r := &OSSReceiver{seen: make(map[string]string), seenLimit: 2}
	r.markSeen("a.txt", "sig-a")
	r.markSeen("b.txt", "sig-b")
	r.markSeen("c.txt", "sig-c")
	if r.isSeen("a.txt", "sig-a") {
		t.Fatalf("oldest seen entry should be evicted")
	}
	if !r.isSeen("b.txt", "sig-b") || !r.isSeen("c.txt", "sig-c") {
		t.Fatalf("recent seen entries should remain")
	}
	if len(r.seen) != 2 {
		t.Fatalf("seen map should stay bounded, got %d", len(r.seen))
	}
}

func TestCompileOSSMatchKeyBuilderModes(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.ReceiverMatchKeyConfig
		key  string
		mode string
		want string
	}{
		{name: "compat", key: "dir/a|b.csv", mode: receiverMatchKeyModeCompatDefault, want: `oss|bucket=bucket\|x|key=dir/a\|b.csv`},
		{name: "remote path", cfg: config.ReceiverMatchKeyConfig{Mode: "remote_path"}, key: "dir/a.csv", mode: "remote_path", want: "oss|remote_path=dir/a.csv"},
		{name: "filename", cfg: config.ReceiverMatchKeyConfig{Mode: "filename"}, key: "dir/a.csv", mode: "filename", want: "oss|filename=a.csv"},
		{name: "fixed", cfg: config.ReceiverMatchKeyConfig{Mode: "fixed", FixedValue: "fixed|v"}, key: "ignored", mode: "fixed", want: `oss|fixed=fixed\|v`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, mode, err := compileOSSMatchKeyBuilder(tt.cfg, "bucket|x")
			if err != nil {
				t.Fatalf("compile oss match key: %v", err)
			}
			if mode != tt.mode {
				t.Fatalf("mode got=%q want=%q", mode, tt.mode)
			}
			if got := builder(tt.key); got != tt.want {
				t.Fatalf("match key got=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestCompileOSSMatchKeyBuilderRejectsUnknownMode(t *testing.T) {
	if _, _, err := compileOSSMatchKeyBuilder(config.ReceiverMatchKeyConfig{Mode: "etag"}, "bucket"); err == nil {
		t.Fatalf("expected unsupported oss match_key mode error")
	}
}
