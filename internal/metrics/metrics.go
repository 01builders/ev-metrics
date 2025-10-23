package metrics

import (
	"fmt"
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics contains Prometheus metrics for DA verification failures
type Metrics struct {
	// UnsubmittedRangeStart tracks the start of unsubmitted block ranges.
	UnsubmittedRangeStart *prometheus.GaugeVec
	// UnsubmittedRangeEnd tracks the end of unsubmitted block ranges.
	UnsubmittedRangeEnd *prometheus.GaugeVec
	// UnsubmittedBlocksTotal tracks the total number of unsubmitted blocks.
	UnsubmittedBlocksTotal *prometheus.GaugeVec
	// ReferenceBlockHeight tracks the block height for reference endpoint (sequencer).
	ReferenceBlockHeight *prometheus.GaugeVec
	// CurrentBlockHeight tracks the block height for target endpoints (operator nodes).
	CurrentBlockHeight *prometheus.GaugeVec
	// BlockHeightDrift tracks the drift between reference and target endpoints for a specific node.
	BlockHeightDrift *prometheus.GaugeVec
	// SubmissionDaHeight tracks the DA height at which blocks were submitted.
	SubmissionDaHeight *prometheus.GaugeVec

	// internal tracking to ensure we only record increasing DA heights
	latestHeaderDaHeight uint64
	latestDataDaHeight   uint64

	mu     sync.Mutex
	ranges map[string][]*blockRange // key: blobType -> sorted slice of ranges
}

type blockRange struct {
	start uint64
	end   uint64
}

// New creates a new Metrics instance using the default Prometheus registry
func New(namespace string) *Metrics {
	return NewWithRegistry(namespace, prometheus.DefaultRegisterer)
}

// NewWithRegistry creates a new Metrics instance with a custom registry
func NewWithRegistry(namespace string, registerer prometheus.Registerer) *Metrics {
	factory := promauto.With(registerer)
	return &Metrics{
		UnsubmittedRangeStart: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "unsubmitted_block_range_start",
				Help:      "start of unsubmitted block range",
			},
			[]string{"chain_id", "blob_type", "range_id"},
		),
		UnsubmittedRangeEnd: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "unsubmitted_block_range_end",
				Help:      "end of unsubmitted block range",
			},
			[]string{"chain_id", "blob_type", "range_id"},
		),
		UnsubmittedBlocksTotal: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "unsubmitted_blocks_total",
				Help:      "total number of unsubmitted blocks",
			},
			[]string{"chain_id", "blob_type"},
		),
		ReferenceBlockHeight: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "reference_block_height",
				Help:      "current block height of the reference endpoint (sequencer)",
			},
			[]string{"chain_id", "endpoint"},
		),
		CurrentBlockHeight: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "target_block_height",
				Help:      "current block height of target endpoints (operator nodes)",
			},
			[]string{"chain_id", "endpoint"},
		),
		BlockHeightDrift: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "block_height_drift",
				Help:      "block height difference between reference and target endpoints (positive = target behind, negative = target ahead)",
			},
			[]string{"chain_id", "target_endpoint"},
		),
		SubmissionDaHeight: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "submission_da_height",
				Help:      "latest DA height for header and data submissions",
			},
			[]string{"chain_id", "type"},
		),
		ranges: make(map[string][]*blockRange),
	}
}

// RecordSubmissionDaHeight records the DA height only if it's higher than previously recorded
func (m *Metrics) RecordSubmissionDaHeight(chainID, submissionType string, daHeight uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if submissionType == "header" {
		if daHeight > m.latestHeaderDaHeight {
			m.latestHeaderDaHeight = daHeight
			m.SubmissionDaHeight.WithLabelValues(chainID, "header").Set(float64(daHeight))
		}
		return
	}

	if submissionType == "data" {
		if daHeight > m.latestDataDaHeight {
			m.latestDataDaHeight = daHeight
			m.SubmissionDaHeight.WithLabelValues(chainID, "data").Set(float64(daHeight))
		}
	}
}

// RecordTotalMissingBlocks updates the total count of missing blocks metric.
func (m *Metrics) RecordTotalMissingBlocks(chainID, blobType string) {
	ranges := m.ranges[blobType]
	total := uint64(0)
	for _, r := range ranges {
		total += r.end - r.start + 1 // inclusive count
	}
	m.UnsubmittedBlocksTotal.WithLabelValues(chainID, blobType).Set(float64(total))
}

// RecordMissingBlock records a block that is missing from Celestia
func (m *Metrics) RecordMissingBlock(chainID, blobType string, blockHeight uint64) {
	m.mu.Lock()
	defer func() {
		m.RecordTotalMissingBlocks(chainID, blobType)
		m.mu.Unlock()
	}()

	ranges := m.ranges[blobType]
	if ranges == nil {
		ranges = []*blockRange{}
	}

	// find the position where this block should be inserted or merged
	idx := m.findRangeIndex(ranges, blockHeight)

	// check if block is already in a range
	if idx < len(ranges) && blockHeight >= ranges[idx].start && blockHeight <= ranges[idx].end {
		// block already tracked
		return
	}

	// check if block extends an existing range
	canMergeLeft := idx > 0 && ranges[idx-1].end+1 == blockHeight
	canMergeRight := idx < len(ranges) && ranges[idx].start-1 == blockHeight

	if canMergeLeft && canMergeRight {
		// merge two ranges
		leftRange := ranges[idx-1]
		rightRange := ranges[idx]

		m.deleteRange(chainID, blobType, leftRange)
		m.deleteRange(chainID, blobType, rightRange)

		// extend left range to include right range
		leftRange.end = rightRange.end
		m.updateRange(chainID, blobType, leftRange)

		// remove right range from slice
		m.ranges[blobType] = append(ranges[:idx], ranges[idx+1:]...)
		return
	}

	if canMergeLeft {
		// extend left range
		leftRange := ranges[idx-1]
		m.deleteRange(chainID, blobType, leftRange)
		leftRange.end = blockHeight
		m.updateRange(chainID, blobType, leftRange)
		return
	}

	if canMergeRight {
		// extend right range
		rightRange := ranges[idx]
		m.deleteRange(chainID, blobType, rightRange)
		rightRange.start = blockHeight
		m.updateRange(chainID, blobType, rightRange)
		return
	}

	// create new range
	newRange := &blockRange{
		start: blockHeight,
		end:   blockHeight,
	}
	// insert at idx
	ranges = append(ranges[:idx], append([]*blockRange{newRange}, ranges[idx:]...)...)
	m.updateRange(chainID, blobType, newRange)
	m.ranges[blobType] = ranges
}

// RemoveVerifiedBlock removes a block from the missing ranges when it gets verified
func (m *Metrics) RemoveVerifiedBlock(chainID, blobType string, blockHeight uint64) {
	m.mu.Lock()
	defer func() {
		m.RecordTotalMissingBlocks(chainID, blobType)
		m.mu.Unlock()
	}()

	ranges := m.ranges[blobType]
	if ranges == nil {
		return
	}

	// find the range containing this block
	idx := m.findRangeIndex(ranges, blockHeight)
	if idx >= len(ranges) {
		return
	}

	r := ranges[idx]

	// block not in any range, don't do anything.
	if blockHeight < r.start || blockHeight > r.end {
		return
	}

	// range contains only this block, delete it.
	if r.start == r.end {
		m.deleteRange(chainID, blobType, r)
		// remove range from slice
		m.ranges[blobType] = append(ranges[:idx], ranges[idx+1:]...)
		return
	}

	// block is at start of range, shrink the range
	if blockHeight == r.start {
		// remove from start of range
		m.deleteRange(chainID, blobType, r)
		r.start++ // modify existing range
		m.updateRange(chainID, blobType, r)
		return
	}

	// block is at end of range, shrink the range
	if blockHeight == r.end {
		// remove from end of range
		m.deleteRange(chainID, blobType, r)
		r.end-- // modify existing range
		m.updateRange(chainID, blobType, r)
		return
	}

	// block is in middle of range, split into two ranges
	oldEnd := r.end
	m.deleteRange(chainID, blobType, r)

	// update first range
	r.end = blockHeight - 1
	m.updateRange(chainID, blobType, r)

	// create new range for the second part
	newRange := &blockRange{
		start: blockHeight + 1,
		end:   oldEnd,
	}
	// insert after current range
	ranges = append(ranges[:idx+1], append([]*blockRange{newRange}, ranges[idx+1:]...)...)
	m.updateRange(chainID, blobType, newRange)

	m.ranges[blobType] = ranges
}

func (m *Metrics) updateRange(chain, blobType string, r *blockRange) {
	rangeID := m.rangeID(r)
	m.UnsubmittedRangeStart.WithLabelValues(chain, blobType, rangeID).Set(float64(r.start))
	m.UnsubmittedRangeEnd.WithLabelValues(chain, blobType, rangeID).Set(float64(r.end))
}

func (m *Metrics) deleteRange(chain, blobType string, r *blockRange) {
	rangeID := m.rangeID(r)
	m.UnsubmittedRangeStart.DeleteLabelValues(chain, blobType, rangeID)
	m.UnsubmittedRangeEnd.DeleteLabelValues(chain, blobType, rangeID)
}

// findRangeIndex finds the index of the range containing blockHeight using sort.Search
// Returns the index where blockHeight belongs (either in an existing range or insertion point)
func (m *Metrics) findRangeIndex(ranges []*blockRange, blockHeight uint64) int {
	// find the first range where start > blockHeight
	idx := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].start > blockHeight
	})

	// if idx > 0, check if blockHeight is in the previous range
	if idx > 0 && blockHeight <= ranges[idx-1].end {
		return idx - 1
	}

	return idx
}

// rangeID generates the Prometheus label value for the range
func (m *Metrics) rangeID(r *blockRange) string {
	return fmt.Sprintf("%d-%d", r.start, r.end)
}

// RecordReferenceBlockHeight records the current block height of the reference endpoint
func (m *Metrics) RecordReferenceBlockHeight(chainID, endpoint string, height uint64) {
	m.ReferenceBlockHeight.WithLabelValues(chainID, endpoint).Set(float64(height))
}

// RecordCurrentBlockHeight records the current block height of a target endpoint
func (m *Metrics) RecordCurrentBlockHeight(chainID, endpoint string, height uint64) {
	m.CurrentBlockHeight.WithLabelValues(chainID, endpoint).Set(float64(height))
}

// RecordBlockHeightDrift calculates and records the drift between reference and target
func (m *Metrics) RecordBlockHeightDrift(chainID, targetEndpoint string, referenceHeight, targetHeight uint64) {
	drift := int64(referenceHeight) - int64(targetHeight)
	m.BlockHeightDrift.WithLabelValues(chainID, targetEndpoint).Set(float64(drift))
}
