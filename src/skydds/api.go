package skydds

import "time"

type CommonOptions struct {
	DCPSConfigFile string
	DomainID       int
	TopicName      string
	MessageModel   string
}

type Writer interface {
	Write(payload []byte) error
	WriteBatch(payloads [][]byte) error
	Close() error
}

type Reader interface {
	Poll(timeout time.Duration) ([]byte, error)
	PollBatch(timeout time.Duration) ([][]byte, error)
	Close() error
}

func NewWriter(opts CommonOptions) (Writer, error) { return newWriter(opts) }
func NewReader(opts CommonOptions) (Reader, error) { return newReader(opts) }
