//go:build skydds

package skydds

/*
// Prefer the vendored SkyDDS/ACE/TAO SDK tree to avoid mixing system headers
// or linker search paths with the SDK-provided userspace baseline.
#cgo CXXFLAGS: -std=c++17 -I${SRCDIR} -I${SRCDIR}/../../third_party/skydds/sdk/include -I${SRCDIR}/../../third_party/skydds/sdk/ACE_wrappers -I${SRCDIR}/../../third_party/skydds/sdk/ACE_wrappers/TAO -I${SRCDIR}/../../third_party/skydds/sdk/examples/SatelliteBatchMsg
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/skydds/sdk/lib -L${SRCDIR}/../../third_party/skydds/sdk/DDS/lib -L${SRCDIR}/../../third_party/skydds/sdk/ACE_wrappers/lib -lSkyDDS_Dcps -lSkyDDS_Tcp -lSkyDDS_Rtps_Udp -lSkyDDS_InfoRepoDiscovery -lTAO_PortableServer -lTAO_AnyTypeCode -lTAO -lACE -lSatelliteCommon -ldl -lpthread
#include <stdlib.h>
#include "skydds_bridge.h"
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

type cgoWriter struct{ ptr *C.skydds_writer_t }

type cgoReader struct {
	ptr              *C.skydds_reader_t
	model            string
	drainBufferBytes int
}

func newWriter(opts CommonOptions) (Writer, error) {
	copts := buildCOptions(opts)
	defer freeCOptions(copts)
	var ptr *C.skydds_writer_t
	var errBuf [512]C.char
	if code := C.skydds_writer_open(&copts, &ptr, &errBuf[0], C.int(len(errBuf))); code != 0 {
		return nil, fmt.Errorf("skydds writer open failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
	return &cgoWriter{ptr: ptr}, nil
}

func newReader(opts CommonOptions) (Reader, error) {
	copts := buildCOptions(opts)
	defer freeCOptions(copts)
	var ptr *C.skydds_reader_t
	var errBuf [512]C.char
	if code := C.skydds_reader_open(&copts, &ptr, &errBuf[0], C.int(len(errBuf))); code != 0 {
		return nil, fmt.Errorf("skydds reader open failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
	return &cgoReader{
		ptr:              ptr,
		model:            opts.MessageModel,
		drainBufferBytes: normalizeDrainBufferBytes(opts.DrainBufferBytes),
	}, nil
}

func (w *cgoWriter) Write(payload []byte) error {
	if w == nil || w.ptr == nil {
		return fmt.Errorf("skydds writer is nil")
	}
	if len(payload) == 0 {
		return nil
	}
	var errBuf [512]C.char
	if code := C.skydds_writer_send(w.ptr, (*C.uint8_t)(unsafe.Pointer(&payload[0])), C.int(len(payload)), &errBuf[0], C.int(len(errBuf))); code != 0 {
		return fmt.Errorf("skydds writer send failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
	return nil
}

func (w *cgoWriter) WriteBatch(payloads [][]byte) error {
	if w == nil || w.ptr == nil {
		return fmt.Errorf("skydds writer is nil")
	}
	if len(payloads) == 0 {
		return nil
	}

	count := len(payloads)
	ptrBytes := make([]*C.uint8_t, count)
	cLens := make([]C.int, count)
	allocs := make([]unsafe.Pointer, 0, count)
	defer func() {
		for _, p := range allocs {
			C.free(p)
		}
	}()

	for i := range payloads {
		cLens[i] = C.int(len(payloads[i]))
		if len(payloads[i]) == 0 {
			ptrBytes[i] = nil
			continue
		}
		p := C.CBytes(payloads[i])
		allocs = append(allocs, p)
		ptrBytes[i] = (*C.uint8_t)(p)
	}

	var errBuf [512]C.char
	if code := C.skydds_writer_send_batch(
		w.ptr,
		(**C.uint8_t)(unsafe.Pointer(&ptrBytes[0])),
		(*C.int)(unsafe.Pointer(&cLens[0])),
		C.int(count),
		&errBuf[0],
		C.int(len(errBuf)),
	); code != 0 {
		return fmt.Errorf("skydds writer send_batch failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
	return nil
}

func (w *cgoWriter) Close() error {
	if w == nil || w.ptr == nil {
		return nil
	}
	C.skydds_writer_close(w.ptr)
	w.ptr = nil
	return nil
}

func (r *cgoReader) Poll(timeout time.Duration) ([]byte, error) {
	items, err := r.Drain(1)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return items[0], nil
	}
	ok, err := r.Wait(timeout)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	items, err = r.Drain(1)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func (r *cgoReader) Wait(timeout time.Duration) (bool, error) {
	if r == nil || r.ptr == nil {
		return false, fmt.Errorf("skydds reader is nil")
	}
	var errBuf [512]C.char
	code := C.skydds_reader_wait(r.ptr, C.int(timeout.Milliseconds()), &errBuf[0], C.int(len(errBuf)))
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	case -2:
		return false, nil
	default:
		return false, fmt.Errorf("skydds reader wait failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
}

func (r *cgoReader) Drain(maxItems int) ([][]byte, error) {
	if r == nil || r.ptr == nil {
		return nil, fmt.Errorf("skydds reader is nil")
	}
	if maxItems <= 0 {
		maxItems = 2048
	}
	buf := make([]byte, r.drainBufferBytes)
	lens := make([]C.int, maxItems)
	var outCount C.int
	var outTotal C.int
	var errBuf [512]C.char
	code := C.skydds_reader_drain(
		r.ptr,
		(*C.uint8_t)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
		(*C.int)(unsafe.Pointer(&lens[0])),
		C.int(len(lens)),
		C.int(maxItems),
		&outCount,
		&outTotal,
		&errBuf[0],
		C.int(len(errBuf)),
	)
	if code != 0 {
		return nil, fmt.Errorf("skydds reader drain failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
	if outCount <= 0 {
		return nil, nil
	}
	out := make([][]byte, 0, int(outCount))
	off := 0
	for i := 0; i < int(outCount); i++ {
		l := int(lens[i])
		if l < 0 || off+l > int(outTotal) || off+l > len(buf) {
			return nil, fmt.Errorf("invalid batch payload layout")
		}
		out = append(out, append([]byte(nil), buf[off:off+l]...))
		off += l
	}
	return out, nil
}

func (r *cgoReader) PollBatch(timeout time.Duration) ([][]byte, error) {
	items, err := r.Drain(2048)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		if r.model == "batch_octet" {
			return items, nil
		}
		return items[:1], nil
	}
	ok, err := r.Wait(timeout)
	if err != nil || !ok {
		return nil, err
	}
	return r.Drain(2048)
}

func (r *cgoReader) Close() error {
	if r == nil || r.ptr == nil {
		return nil
	}
	C.skydds_reader_close(r.ptr)
	r.ptr = nil
	return nil
}

func buildCOptions(opts CommonOptions) C.skydds_common_options_t {
	return C.skydds_common_options_t{
		dcps_config_file: C.CString(opts.DCPSConfigFile),
		domain_id:        C.int(opts.DomainID),
		topic_name:       C.CString(opts.TopicName),
		message_model:    C.CString(opts.MessageModel),
	}
}

func freeCOptions(opts C.skydds_common_options_t) {
	C.free(unsafe.Pointer(opts.dcps_config_file))
	C.free(unsafe.Pointer(opts.topic_name))
	C.free(unsafe.Pointer(opts.message_model))
}
