package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestMetrics_RecordMissingBlock(t *testing.T) {
	tests := []struct {
		name           string
		blocks         []blockToRecord
		expectedRanges []expectedRange
	}{
		{
			name: "single block creates single range",
			blocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 100},
			},
		},
		{
			name: "adjacent blocks create single range",
			blocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 102},
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 102},
			},
		},
		{
			name: "extend range backward",
			blocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 100},
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 102},
			},
		},
		{
			name: "multiple disjoint ranges",
			blocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 200},
				{chain: "testchain", blobType: "header", height: 201},
				{chain: "testchain", blobType: "header", height: 202},
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 102},
				{blobType: "header", start: 200, end: 202},
			},
		},
		{
			name: "different blob types create separate ranges",
			blocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "data", height: 100},
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 100},
				{blobType: "data", start: 100, end: 100},
			},
		},
		{
			name: "extend range at both ends",
			blocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 103},
				{chain: "testchain", blobType: "header", height: 101}, // extend backward
				{chain: "testchain", blobType: "header", height: 104}, // extend forward
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 101, end: 104},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m := NewWithRegistry("test", reg)

			// record all blocks
			for _, block := range tt.blocks {
				m.RecordMissingBlock(block.chain, block.blobType, block.height)
			}

			// count total ranges across all blob types
			totalRanges := 0
			for _, ranges := range m.ranges {
				totalRanges += len(ranges)
			}
			require.Equal(t, len(tt.expectedRanges), totalRanges, "unexpected number of ranges")

			for _, expected := range tt.expectedRanges {
				found := false
				for blobType, ranges := range m.ranges {
					if blobType == expected.blobType {
						for _, r := range ranges {
							if r.start == expected.start && r.end == expected.end {
								found = true
								break
							}
						}
					}
					if found {
						break
					}
				}
				require.True(t, found, "expected to find range %s [%d-%d]", expected.blobType, expected.start, expected.end)
			}
		})
	}
}

func TestMetrics_RemoveVerifiedBlock(t *testing.T) {
	tests := []struct {
		name            string
		setupBlocks     []blockToRecord
		removeBlock     blockToRemove
		expectedRanges  []expectedRange
		expectNoRanges  bool
	}{
		{
			name: "remove single block range - deletes range",
			setupBlocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
			},
			removeBlock: blockToRemove{
				chain:    "testchain",
				blobType: "header",
				height:   100,
			},
			expectNoRanges: true,
		},
		{
			name: "remove from start of range",
			setupBlocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 103},
				{chain: "testchain", blobType: "header", height: 104},
				{chain: "testchain", blobType: "header", height: 105},
			},
			removeBlock: blockToRemove{
				chain:    "testchain",
				blobType: "header",
				height:   100,
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 101, end: 105},
			},
		},
		{
			name: "remove from end of range",
			setupBlocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 103},
				{chain: "testchain", blobType: "header", height: 104},
				{chain: "testchain", blobType: "header", height: 105},
			},
			removeBlock: blockToRemove{
				chain:    "testchain",
				blobType: "header",
				height:   105,
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 104},
			},
		},
		{
			name: "remove from middle - splits range",
			setupBlocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 103},
				{chain: "testchain", blobType: "header", height: 104},
				{chain: "testchain", blobType: "header", height: 105},
			},
			removeBlock: blockToRemove{
				chain:    "testchain",
				blobType: "header",
				height:   103,
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 102},
				{blobType: "header", start: 104, end: 105},
			},
		},
		{
			name: "remove non-existent block - no effect",
			setupBlocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
				{chain: "testchain", blobType: "header", height: 102},
				{chain: "testchain", blobType: "header", height: 103},
				{chain: "testchain", blobType: "header", height: 104},
				{chain: "testchain", blobType: "header", height: 105},
			},
			removeBlock: blockToRemove{
				chain:    "testchain",
				blobType: "header",
				height:   200,
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 105},
			},
		},
		{
			name: "remove from different blob type - no effect",
			setupBlocks: []blockToRecord{
				{chain: "testchain", blobType: "header", height: 100},
				{chain: "testchain", blobType: "header", height: 101},
			},
			removeBlock: blockToRemove{
				chain:    "testchain",
				blobType: "data",
				height:   100,
			},
			expectedRanges: []expectedRange{
				{blobType: "header", start: 100, end: 101},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m := NewWithRegistry("test", reg)

			// setup: record all blocks
			for _, block := range tt.setupBlocks {
				m.RecordMissingBlock(block.chain, block.blobType, block.height)
			}

			// action: remove block
			m.RemoveVerifiedBlock(tt.removeBlock.chain, tt.removeBlock.blobType, tt.removeBlock.height)

			// verify
			if tt.expectNoRanges {
				totalRanges := 0
				for _, ranges := range m.ranges {
					totalRanges += len(ranges)
				}
				require.Equal(t, 0, totalRanges, "expected no ranges")
			} else {
				totalRanges := 0
				for _, ranges := range m.ranges {
					totalRanges += len(ranges)
				}
				require.Equal(t, len(tt.expectedRanges), totalRanges, "unexpected number of ranges")

				for _, expected := range tt.expectedRanges {
					found := false
					for blobType, ranges := range m.ranges {
						if blobType == expected.blobType {
							for _, r := range ranges {
								if r.start == expected.start && r.end == expected.end {
									found = true
									break
								}
							}
						}
						if found {
							break
						}
					}
					require.True(t, found, "expected to find range %s [%d-%d]", expected.blobType, expected.start, expected.end)
				}
			}
		})
	}
}

func TestMetrics_ComplexScenario(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWithRegistry("test", reg)

	// create initial range 100-110
	for i := uint64(100); i <= 110; i++ {
		m.RecordMissingBlock("testchain", "header", i)
	}

	totalRanges := 0
	for _, ranges := range m.ranges {
		totalRanges += len(ranges)
	}
	require.Equal(t, 1, totalRanges, "should start with one range")

	// remove block 103 - splits into two ranges
	m.RemoveVerifiedBlock("testchain", "header", 103)
	totalRanges = 0
	for _, ranges := range m.ranges {
		totalRanges += len(ranges)
	}
	require.Equal(t, 2, totalRanges, "should have two ranges after first split")

	// remove block 107 - splits second range
	m.RemoveVerifiedBlock("testchain", "header", 107)
	totalRanges = 0
	for _, ranges := range m.ranges {
		totalRanges += len(ranges)
	}
	require.Equal(t, 3, totalRanges, "should have three ranges after second split")

	// verify final ranges: 100-102, 104-106, 108-110
	expectedRanges := []expectedRange{
		{blobType: "header", start: 100, end: 102},
		{blobType: "header", start: 104, end: 106},
		{blobType: "header", start: 108, end: 110},
	}

	for _, expected := range expectedRanges {
		found := false
		for blobType, ranges := range m.ranges {
			if blobType == expected.blobType {
				for _, r := range ranges {
					if r.start == expected.start && r.end == expected.end {
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		require.True(t, found, "expected to find range [%d-%d]", expected.start, expected.end)
	}

	// remove all blocks from first range
	m.RemoveVerifiedBlock("testchain", "header", 100)
	m.RemoveVerifiedBlock("testchain", "header", 101)
	m.RemoveVerifiedBlock("testchain", "header", 102)

	totalRanges = 0
	for _, ranges := range m.ranges {
		totalRanges += len(ranges)
	}
	require.Equal(t, 2, totalRanges, "should have two ranges after removing first range")

	// verify remaining ranges: 104-106, 108-110
	expectedRanges = []expectedRange{
		{blobType: "header", start: 104, end: 106},
		{blobType: "header", start: 108, end: 110},
	}

	for _, expected := range expectedRanges {
		found := false
		for blobType, ranges := range m.ranges {
			if blobType == expected.blobType {
				for _, r := range ranges {
					if r.start == expected.start && r.end == expected.end {
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		require.True(t, found, "expected to find range [%d-%d]", expected.start, expected.end)
	}
}

// helper types for table tests
type blockToRecord struct {
	chain    string
	blobType string
	height   uint64
}

type blockToRemove struct {
	chain    string
	blobType string
	height   uint64
}

type expectedRange struct {
	blobType string
	start    uint64
	end      uint64
}
