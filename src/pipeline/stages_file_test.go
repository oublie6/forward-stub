package pipeline

import (
	"regexp"
	"testing"

	"forward-stub/src/packet"
)

func TestFileTargetPathStages(t *testing.T) {
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{FilePath: "/in/raw/a.csv", FileName: "a.csv"}}}
	SetTargetFilePath("/in/raw/a.csv")([]*packet.Packet{p})
	RewriteTargetPathStripPrefix("/in/")([]*packet.Packet{p})
	if p.Meta.TargetFilePath != "raw/a.csv" {
		t.Fatalf("strip got=%q", p.Meta.TargetFilePath)
	}
	RewriteTargetPathAddPrefix("out")([]*packet.Packet{p})
	if p.Meta.TargetFilePath != "out/raw/a.csv" {
		t.Fatalf("add got=%q", p.Meta.TargetFilePath)
	}
	RewriteTargetFilenameReplace(".csv", "_done.csv")([]*packet.Packet{p})
	if p.Meta.TargetFileName != "a_done.csv" || p.Meta.TargetFilePath != "out/raw/a_done.csv" {
		t.Fatalf("replace name=%q path=%q", p.Meta.TargetFileName, p.Meta.TargetFilePath)
	}
}

func TestFileTargetRegexStages(t *testing.T) {
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{FilePath: "raw/2026/a.csv", FileName: "a.csv"}}}
	RewriteTargetPathRegex(regexp.MustCompile(`raw/(\d+)/`), `out/$1/`)([]*packet.Packet{p})
	if p.Meta.TargetFilePath != "out/2026/a.csv" {
		t.Fatalf("path regex got=%q", p.Meta.TargetFilePath)
	}
	RewriteTargetFilenameRegex(regexp.MustCompile(`^(.+)\.csv$`), `${1}.ready.csv`)([]*packet.Packet{p})
	if p.Meta.TargetFileName != "a.ready.csv" || p.Meta.TargetFilePath != "out/2026/a.ready.csv" {
		t.Fatalf("filename regex name=%q path=%q", p.Meta.TargetFileName, p.Meta.TargetFilePath)
	}
}

func TestFileTargetPathBoundaryStages(t *testing.T) {
	tests := []struct {
		name string
		run  func(*packet.Packet)
		want string
	}{
		{
			name: "strip prefix missing keeps path but normalizes leading slash",
			run:  func(p *packet.Packet) { RewriteTargetPathStripPrefix("/missing/")([]*packet.Packet{p}) },
			want: "in/raw/a.csv",
		},
		{
			name: "add prefix cleans repeated slashes",
			run:  func(p *packet.Packet) { RewriteTargetPathAddPrefix("//out//ready//")([]*packet.Packet{p}) },
			want: "out/ready/in/raw/a.csv",
		},
		{
			name: "path regex no match keeps source path",
			run: func(p *packet.Packet) {
				RewriteTargetPathRegex(regexp.MustCompile(`^no-match/`), "out/")([]*packet.Packet{p})
			},
			want: "/in/raw/a.csv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{FilePath: "/in/raw/a.csv", FileName: "a.csv"}}}
			tt.run(p)
			if p.Meta.TargetFilePath != tt.want {
				t.Fatalf("target path got=%q want=%q", p.Meta.TargetFilePath, tt.want)
			}
		})
	}
}

func TestFilenameRewriteFallsBackToPathBaseAndKeepsEmptyNamePath(t *testing.T) {
	// 没有 FileName 时应从 FilePath 的 basename 推导文件名，并同步改写 TargetFilePath。
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{FilePath: "raw/deep/a.csv"}}}
	RewriteTargetFilenameReplace(".csv", ".ready")([]*packet.Packet{p})
	if p.Meta.TargetFileName != "a.ready" || p.Meta.TargetFilePath != "raw/deep/a.ready" {
		t.Fatalf("fallback filename rewrite name=%q path=%q", p.Meta.TargetFileName, p.Meta.TargetFilePath)
	}

	empty := &packet.Packet{}
	RewriteTargetFilenameReplace("x", "y")([]*packet.Packet{empty})
	if empty.Meta.TargetFileName != "" || empty.Meta.TargetFilePath != "" {
		t.Fatalf("empty packet rewrite should not create pseudo filename, name=%q path=%q", empty.Meta.TargetFileName, empty.Meta.TargetFilePath)
	}

	emptyRegex := &packet.Packet{}
	RewriteTargetFilenameRegex(regexp.MustCompile(`^(.+)\.csv$`), `${1}.ready.csv`)([]*packet.Packet{emptyRegex})
	if emptyRegex.Meta.TargetFileName != "" || emptyRegex.Meta.TargetFilePath != "" {
		t.Fatalf("empty packet regex rewrite should not create pseudo filename, name=%q path=%q", emptyRegex.Meta.TargetFileName, emptyRegex.Meta.TargetFilePath)
	}
}

func TestPathRegexKeepsLeadingSlashCurrentSemantics(t *testing.T) {
	p := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{FilePath: "/in/raw/a.csv"}}}
	RewriteTargetPathRegex(regexp.MustCompile(`^/in/`), "/out/")([]*packet.Packet{p})
	if p.Meta.TargetFilePath != "/out/raw/a.csv" {
		t.Fatalf("path regex should keep current leading slash semantics, got=%q", p.Meta.TargetFilePath)
	}
}
