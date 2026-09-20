package observability

import "sync/atomic"

type Metrics struct {
	requests atomic.Uint64
	failures atomic.Uint64
	writes   atomic.Uint64
}

func (m *Metrics) Request() { m.requests.Add(1) }
func (m *Metrics) Failure() { m.failures.Add(1) }
func (m *Metrics) Write()   { m.writes.Add(1) }

type Snapshot struct {
	Requests uint64 `json:"requests"`
	Failures uint64 `json:"failures"`
	Writes   uint64 `json:"writes"`
}

func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{Requests: m.requests.Load(), Failures: m.failures.Load(), Writes: m.writes.Load()}
}
