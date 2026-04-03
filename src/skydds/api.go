package skydds

import "time"

const DefaultDrainBufferBytes = 4 << 20

type CommonOptions struct {
	DCPSConfigFile   string
	DomainID         int
	TopicName        string
	MessageModel     string
	DrainBufferBytes int
}

func normalizeDrainBufferBytes(v int) int {
	if v <= 0 {
		return DefaultDrainBufferBytes
	}
	return v
}

type Writer interface {
	Write(payload []byte) error
	WriteBatch(payloads [][]byte) error
	Close() error
}

type Reader interface {
	Wait(timeout time.Duration) (bool, error)
	Drain(maxItems int) ([][]byte, error)
	Poll(timeout time.Duration) ([]byte, error)
	PollBatch(timeout time.Duration) ([][]byte, error)
	Close() error
}

func NewWriter(opts CommonOptions) (Writer, error) { return newWriter(opts) }
func NewReader(opts CommonOptions) (Reader, error) { return newReader(opts) }
