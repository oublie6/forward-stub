//go:build !skydds

package skydds

import (
	"fmt"
	"time"
)

type stubWriter struct{}

func (stubWriter) Write([]byte) error {
	return fmt.Errorf("skydds support disabled: build with -tags skydds and CGO_ENABLED=1")
}
func (stubWriter) Close() error { return nil }

type stubReader struct{}

func (stubReader) Poll(time.Duration) ([]byte, error) {
	return nil, fmt.Errorf("skydds support disabled: build with -tags skydds and CGO_ENABLED=1")
}
func (stubReader) Close() error { return nil }

func newWriter(CommonOptions) (Writer, error) {
	return nil, fmt.Errorf("skydds support disabled: build with -tags skydds and CGO_ENABLED=1")
}

func newReader(CommonOptions) (Reader, error) {
	return nil, fmt.Errorf("skydds support disabled: build with -tags skydds and CGO_ENABLED=1")
}
