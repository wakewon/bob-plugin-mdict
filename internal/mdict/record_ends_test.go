package mdict

import (
	"testing"

	"github.com/lib-x/mdx"
)

func TestLinkRecordEndsClosesKeyBlockBoundaries(t *testing.T) {
	// Two key blocks: the engine links 0→1 and 2→3, but leaves 1 (the last
	// entry of the first block) and 3 (the last entry overall) open.
	entries := []*mdx.MDictKeywordEntry{
		{RecordStartOffset: 0, RecordEndOffset: 10},
		{RecordStartOffset: 10},
		{RecordStartOffset: 25, RecordEndOffset: 40},
		{RecordStartOffset: 40},
	}
	linkRecordEnds(entries)
	want := []int64{10, 25, 40, 0}
	for i, entry := range entries {
		if entry.RecordEndOffset != want[i] {
			t.Errorf("entry %d ends at %d, want %d", i, entry.RecordEndOffset, want[i])
		}
	}
}

func TestLinkRecordEndsNeverInventsABackwardRange(t *testing.T) {
	entries := []*mdx.MDictKeywordEntry{{RecordStartOffset: 50}, nil, {RecordStartOffset: 30}, {RecordStartOffset: 20}}
	linkRecordEnds(entries)
	if entries[0].RecordEndOffset != 0 || entries[2].RecordEndOffset != 0 {
		t.Errorf("out-of-order starts produced ends: %d, %d", entries[0].RecordEndOffset, entries[2].RecordEndOffset)
	}
}

func TestLinkRecordEndsSkipsAliasesOfTheSameRecord(t *testing.T) {
	// "colour" and "color" share one record at the end of a key block.
	entries := []*mdx.MDictKeywordEntry{
		{RecordStartOffset: 0, RecordEndOffset: 10},
		{RecordStartOffset: 10},
		{RecordStartOffset: 10},
		{RecordStartOffset: 30},
	}
	linkRecordEnds(entries)
	if entries[1].RecordEndOffset != 30 || entries[2].RecordEndOffset != 30 {
		t.Errorf("aliases end at %d and %d, want 30", entries[1].RecordEndOffset, entries[2].RecordEndOffset)
	}
}
