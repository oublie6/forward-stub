package main

import (
	"encoding/binary"
	"testing"
)

func TestValidateSequenceStrict(t *testing.T) {
	m := &metrics{seqCheck: true}
	payload := make([]byte, 8)

	binary.BigEndian.PutUint64(payload, 10)
	if !validateSequence(payload, m) {
		t.Fatalf("first packet should initialize sequence")
	}
	binary.BigEndian.PutUint64(payload, 11)
	if !validateSequence(payload, m) {
		t.Fatalf("second packet should match expected sequence")
	}
	binary.BigEndian.PutUint64(payload, 13)
	if validateSequence(payload, m) {
		t.Fatalf("gap packet should fail strict sequence check")
	}
}

func TestValidateSequenceDisabled(t *testing.T) {
	if !validateSequence([]byte{1, 2, 3}, &metrics{seqCheck: false}) {
		t.Fatalf("sequence validation disabled should always pass")
	}
}
