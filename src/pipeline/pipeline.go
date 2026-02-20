package pipeline

import "forword-stub/src/packet"

type StageFunc func(*packet.Packet) bool

type Pipeline struct {
	Name   string
	Stages []StageFunc
}

func (pl *Pipeline) Process(p *packet.Packet) bool {
	for _, st := range pl.Stages {
		if !st(p) {
			return false
		}
	}
	return true
}
