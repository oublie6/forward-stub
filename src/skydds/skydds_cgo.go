//go:build skydds

package skydds

/*
#cgo CXXFLAGS: -std=c++17 -I${SRCDIR} -I${SRCDIR}/../../third_party/skydds/sdk/include -I${SRCDIR}/../../third_party/skydds/sdk/examples/SatelliteBatchMsg
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/skydds/sdk/lib -lSkyDDS_Dcps -lSkyDDS_Tcp -lSkyDDS_Rtps_Udp -lSkyDDS_InfoRepoDiscovery -lTAO_PortableServer -lTAO_AnyTypeCode -lTAO -lACE -lSatelliteCommon -ldl -lpthread
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

type cgoReader struct{ ptr *C.skydds_reader_t }

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
	return &cgoReader{ptr: ptr}, nil
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
	if r == nil || r.ptr == nil {
		return nil, fmt.Errorf("skydds reader is nil")
	}
	buf := make([]byte, 1<<20)
	var outLen C.int
	var errBuf [512]C.char
	code := C.skydds_reader_poll(r.ptr, (*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)), C.int(timeout.Milliseconds()), &outLen, &errBuf[0], C.int(len(errBuf)))
	if code == 1 {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("skydds reader poll failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
	}
	if outLen <= 0 {
		return nil, nil
	}
	return append([]byte(nil), buf[:int(outLen)]...), nil
}

func (r *cgoReader) PollBatch(timeout time.Duration) ([][]byte, error) {
	if r == nil || r.ptr == nil {
		return nil, fmt.Errorf("skydds reader is nil")
	}
	buf := make([]byte, 4<<20)
	lens := make([]C.int, 2048)
	var outCount C.int
	var outTotal C.int
	var errBuf [512]C.char
	code := C.skydds_reader_poll_batch(
		r.ptr,
		(*C.uint8_t)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
		(*C.int)(unsafe.Pointer(&lens[0])),
		C.int(len(lens)),
		C.int(timeout.Milliseconds()),
		&outCount,
		&outTotal,
		&errBuf[0],
		C.int(len(errBuf)),
	)
	if code == 1 {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("skydds reader poll_batch failed (code=%d): %s", int(code), C.GoString(&errBuf[0]))
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
