package runtime

import "testing"

func TestRefreshRecvPayloadLogOptionsBuildsSnapshot(t *testing.T) {
	st := NewStore()
	st.mu.Lock()
	st.receivers["r1"] = &ReceiverState{Name: "r1", LogPayloadRecv: true, PayloadLogMax: 128}
	st.receivers["r2"] = &ReceiverState{Name: "r2", LogPayloadRecv: false, PayloadLogMax: 64}
	st.mu.Unlock()

	st.refreshRecvPayloadLogOptions()

	opt, ok := st.getRecvPayloadLogOption("r1")
	if !ok || !opt.enabled || opt.max != 128 {
		t.Fatalf("unexpected r1 log option: ok=%v opt=%+v", ok, opt)
	}
	opt, ok = st.getRecvPayloadLogOption("r2")
	if !ok || opt.enabled || opt.max != 64 {
		t.Fatalf("unexpected r2 log option: ok=%v opt=%+v", ok, opt)
	}
	if _, ok := st.getRecvPayloadLogOption("missing"); ok {
		t.Fatalf("missing receiver should not have log option")
	}
}
