package observability

import "sync/atomic"

type Metrics struct {
	requests         atomic.Uint64
	failures         atomic.Uint64
	writes           atomic.Uint64
	comparisonItems  atomic.Uint64
	comparisonFailed atomic.Uint64
}

func (m *Metrics) Request() { m.requests.Add(1) }
func (m *Metrics) Failure() { m.failures.Add(1) }
func (m *Metrics) Write()   { m.writes.Add(1) }

// ComparisonItem records one item reaching a terminal state. Succeeded and
// failed items both count toward throughput; failures are counted separately.
func (m *Metrics) ComparisonItem(succeeded bool) {
	m.comparisonItems.Add(1)
	if !succeeded {
		m.comparisonFailed.Add(1)
	}
}

type Snapshot struct {
	Requests         uint64 `json:"requests"`
	Failures         uint64 `json:"failures"`
	Writes           uint64 `json:"writes"`
	ComparisonItems  uint64 `json:"comparison_items"`
	ComparisonFailed uint64 `json:"comparison_items_failed"`
}

func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		Requests:         m.requests.Load(),
		Failures:         m.failures.Load(),
		Writes:           m.writes.Load(),
		ComparisonItems:  m.comparisonItems.Load(),
		ComparisonFailed: m.comparisonFailed.Load(),
	}
}
