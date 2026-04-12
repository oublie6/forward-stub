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
