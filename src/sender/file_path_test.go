package sender

import (
	"strings"
	"testing"

	"forward-stub/src/packet"
)

func TestSafeRelativePathNormalizesAndRejectsTraversal(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: ` /a//b\c.txt `, want: "a/b/c.txt"},
		{in: "a/./b.txt", want: "a/b.txt"},
		{in: "", wantErr: true},
		{in: "../secret.txt", wantErr: true},
		{in: "a/../secret.txt", wantErr: true},
	}
	for _, tt := range tests {
		got, err := safeRelativePath(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("safeRelativePath(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Fatalf("safeRelativePath(%q) got=%q err=%v want=%q", tt.in, got, err, tt.want)
		}
	}
}

func TestSFTPFinalPathPriorityAndTargetFilename(t *testing.T) {
	tests := []struct {
		name       string
		meta       packet.Meta
		transferID string
		want       string
	}{
		{
			name: "target path wins over source path and filename",
			meta: packet.Meta{TargetFilePath: "target/a.csv", TargetFileName: "ignored.csv", FilePath: "src/b.csv", FileName: "b.csv"},
			want: "/out/target/a.csv",
		},
		{
			name: "target filename rewrites source path basename when target path is empty",
			meta: packet.Meta{TargetFileName: "done.csv", FilePath: "src/raw/a.csv", FileName: "a.csv"},
			want: "/out/src/raw/done.csv",
		},
		{
			name:       "transfer id fallback",
			meta:       packet.Meta{},
			transferID: "tx-1",
			want:       "/out/tx-1.bin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sftpFinalPath("/out", &packet.Packet{Envelope: packet.Envelope{Meta: tt.meta}}, tt.transferID)
			if err != nil || got != tt.want {
				t.Fatalf("sftpFinalPath got=%q err=%v want=%q", got, err, tt.want)
			}
		})
	}
}

func TestOSSObjectKeyRejectsUnsafePrefixAndAppliesTargetFilename(t *testing.T) {
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{FilePath: "src/raw/a.csv", TargetFileName: "done.csv"}}}
	got, err := ossObjectKey("export", p)
	if err != nil || got != "export/src/raw/done.csv" {
		t.Fatalf("ossObjectKey got=%q err=%v", got, err)
	}

	if _, err := ossObjectKey("../escape", p); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("expected unsafe prefix error, got %v", err)
	}
}
