package base

// Port of U:input/editor/display_map/fold_map.rs: the folding projection
// layer (wrap rows → display rows). The projection is kept as sorted,
// merged runs of hidden wrap rows, so a rebuild costs the number of folds
// and every mapping is a binary search (Rust partition_point →
// sort.Search). Option<usize> results are (int, bool).

import (
	"slices"
	"sort"
)

// hiddenRows is a run of wrap rows hidden by folding
// (U:input/editor/display_map/fold_map.rs:12-25).
type hiddenRows struct {
	start, end int // wrap_rows
	// hiddenBefore is how many wrap rows the runs before this one hide.
	hiddenBefore int
}

// displayStart is the display row of the first visible wrap row after
// this run (U:input/editor/display_map/fold_map.rs:21-24).
func (r hiddenRows) displayStart() int { return r.start - r.hiddenBefore }

// foldMap projects wrap rows to display rows by hiding folded regions
// (U:input/editor/display_map/fold_map.rs:27-57).
type foldMap struct {
	hidden                []hiddenRows
	totalHidden           int
	projectedWrapRowCount int
	// candidates are sorted by start line with unique start lines.
	candidates []FoldRange
	// folded is the subset of candidates that is folded, sorted.
	folded             []FoldRange
	needsRebuild       bool
	cachedWrapRowCount int
}

// newFoldMap is FoldMap::new (U:input/editor/display_map/fold_map.rs:60-70).
func newFoldMap() foldMap { return foldMap{needsRebuild: true} }

// markDirtyWithWrapCount updates the cached wrap row count without a
// rebuild, for the identity mapping of no folds
// (U:input/editor/display_map/fold_map.rs:72-77).
func (m *foldMap) markDirtyWithWrapCount(wrapRowCount int) {
	m.needsRebuild = true
	m.cachedWrapRowCount = wrapRowCount
}

// displayRowCount is U:input/editor/display_map/fold_map.rs:79-85.
func (m *foldMap) displayRowCount() int {
	if len(m.folded) == 0 {
		return m.cachedWrapRowCount
	}
	return m.projectedWrapRowCount - m.totalHidden
}

// hiddenBeforeAt is U:input/editor/display_map/fold_map.rs:87-92.
func (m *foldMap) hiddenBeforeAt(index int) int {
	if index < len(m.hidden) {
		return m.hidden[index].hiddenBefore
	}
	return m.totalHidden
}

// firstRunEndingAfter is hidden.partition_point(|run| run.wrap_rows.end <= wrapRow).
func (m *foldMap) firstRunEndingAfter(wrapRow int) int {
	return sort.Search(len(m.hidden), func(i int) bool { return m.hidden[i].end > wrapRow })
}

// wrapRowToDisplayRow is false for a row hidden by folding
// (U:input/editor/display_map/fold_map.rs:94-118).
func (m *foldMap) wrapRowToDisplayRow(wrapRow int) (int, bool) {
	if len(m.folded) == 0 {
		if wrapRow < m.cachedWrapRowCount {
			return wrapRow, true
		}
		return 0, false
	}
	if wrapRow >= m.projectedWrapRowCount {
		return 0, false
	}
	index := m.firstRunEndingAfter(wrapRow)
	if index < len(m.hidden) && m.hidden[index].start <= wrapRow {
		return 0, false
	}
	return wrapRow - m.hiddenBeforeAt(index), true
}

// displayRowToWrapRow is U:input/editor/display_map/fold_map.rs:120-136.
func (m *foldMap) displayRowToWrapRow(displayRow int) (int, bool) {
	if len(m.folded) == 0 {
		if displayRow < m.cachedWrapRowCount {
			return displayRow, true
		}
		return 0, false
	}
	if displayRow >= m.displayRowCount() {
		return 0, false
	}
	index := sort.Search(len(m.hidden), func(i int) bool { return m.hidden[i].displayStart() > displayRow })
	return displayRow + m.hiddenBeforeAt(index), true
}

// nearestVisibleDisplayRow maps a hidden row to the last visible row
// before it (U:input/editor/display_map/fold_map.rs:138-160).
func (m *foldMap) nearestVisibleDisplayRow(wrapRow int) int {
	if len(m.folded) == 0 {
		return min(wrapRow, max(m.cachedWrapRowCount-1, 0))
	}
	if dr, ok := m.wrapRowToDisplayRow(wrapRow); ok {
		return dr
	}
	visibleBefore := m.displayRowCount()
	if wrapRow < m.projectedWrapRowCount {
		if index := m.firstRunEndingAfter(wrapRow); index < len(m.hidden) {
			visibleBefore = m.hidden[index].displayStart()
		}
	}
	return max(visibleBefore-1, 0)
}

// sortDedupFolds is sort_by_key(start_line) + dedup_by_key(start_line):
// the first of equal start lines is kept.
func sortDedupFolds(ranges []FoldRange) []FoldRange {
	slices.SortStableFunc(ranges, func(a, b FoldRange) int { return a.StartLine - b.StartLine })
	return slices.CompactFunc(ranges, func(a, b FoldRange) bool { return a.StartLine == b.StartLine })
}

// setCandidates replaces the candidates and drops folds no longer among
// them (U:input/editor/display_map/fold_map.rs:162-175).
func (m *foldMap) setCandidates(candidates []FoldRange) {
	m.candidates = sortDedupFolds(slices.Clone(candidates))
	m.folded = slices.DeleteFunc(m.folded, func(fold FoldRange) bool { return !m.isFoldCandidate(fold.StartLine) })
}

// mergeCandidatesForEdit replaces the candidates within the edited lines
// with newCandidates (U:input/editor/display_map/fold_map.rs:177-196).
func (m *foldMap) mergeCandidatesForEdit(editStartLine, editEndLine int, newCandidates []FoldRange) {
	m.candidates = slices.DeleteFunc(m.candidates, func(c FoldRange) bool {
		return !(c.StartLine < editStartLine || c.StartLine > editEndLine)
	})
	m.candidates = sortDedupFolds(append(m.candidates, newCandidates...))
}

// setFolded folds (only a candidate) or unfolds startLine
// (U:input/editor/display_map/fold_map.rs:198-215).
func (m *foldMap) setFolded(startLine int, folded bool) {
	if !folded {
		m.folded = slices.DeleteFunc(m.folded, func(f FoldRange) bool { return f.StartLine == startLine })
		m.needsRebuild = true
		return
	}
	i := slices.IndexFunc(m.candidates, func(c FoldRange) bool { return c.StartLine == startLine })
	if i < 0 || m.isFoldedAt(startLine) {
		return
	}
	m.folded = append(m.folded, m.candidates[i])
	slices.SortStableFunc(m.folded, func(a, b FoldRange) int { return a.StartLine - b.StartLine })
	m.needsRebuild = true
}

// toggleFold is U:input/editor/display_map/fold_map.rs:217-221.
func (m *foldMap) toggleFold(startLine int) { m.setFolded(startLine, !m.isFoldedAt(startLine)) }

// isFoldedAt is U:input/editor/display_map/fold_map.rs:223-226.
func (m *foldMap) isFoldedAt(startLine int) bool {
	return slices.ContainsFunc(m.folded, func(f FoldRange) bool { return f.StartLine == startLine })
}

// isFoldCandidate is U:input/editor/display_map/fold_map.rs:228-231.
func (m *foldMap) isFoldCandidate(startLine int) bool {
	return slices.ContainsFunc(m.candidates, func(c FoldRange) bool { return c.StartLine == startLine })
}

// foldCandidates is U:input/editor/display_map/fold_map.rs:233-237.
func (m *foldMap) foldCandidates() []FoldRange { return m.candidates }

// foldedRanges is U:input/editor/display_map/fold_map.rs:239-243.
func (m *foldMap) foldedRanges() []FoldRange { return m.folded }

// clearFolds is U:input/editor/display_map/fold_map.rs:245-249.
func (m *foldMap) clearFolds() { m.folded = nil }

// adjustFoldsForEdit drops the folds and candidates overlapping the edited
// lines and shifts those after them by lineDelta
// (U:input/editor/display_map/fold_map.rs:251-295).
func (m *foldMap) adjustFoldsForEdit(editStartLine, editEndLine, lineDelta int) {
	adjust := func(ranges []FoldRange) []FoldRange {
		ranges = slices.DeleteFunc(ranges, func(f FoldRange) bool {
			return f.StartLine <= editEndLine && f.EndLine >= editStartLine
		})
		if lineDelta != 0 {
			for i := range ranges {
				if ranges[i].StartLine > editEndLine {
					ranges[i].StartLine = max(ranges[i].StartLine+lineDelta, 0)
					ranges[i].EndLine = max(ranges[i].EndLine+lineDelta, 0)
				}
			}
		}
		return ranges
	}
	if len(m.folded) > 0 {
		m.folded = adjust(m.folded)
	}
	if len(m.candidates) > 0 {
		m.candidates = adjust(m.candidates)
	}
	m.needsRebuild = true
}

// rebuild projects wrap rows to display rows after a wrap map or fold
// change (U:input/editor/display_map/fold_map.rs:297-344).
func (m *foldMap) rebuild(wrap *wrapMap) {
	wrapRowCount := wrap.wrapRowCount()
	if !m.needsRebuild && wrapRowCount == m.cachedWrapRowCount {
		return
	}
	m.cachedWrapRowCount = wrapRowCount
	if len(m.folded) == 0 {
		m.setHiddenRows(wrapRowCount, nil)
		m.needsRebuild = false
		return
	}

	var hidden [][2]int
	for _, fold := range m.folded {
		// Hide start_line+1 ..= end_line-1: both ends stay visible.
		hideStartLine := fold.StartLine + 1
		hideEndLine := max(fold.EndLine-1, 0)
		if hideStartLine > hideEndLine {
			continue
		}
		startWrapRow := wrap.bufferLineToFirstWrapRow(hideStartLine)
		endWrapRow := wrapRowCount
		if hideEndLine+1 < wrap.bufferLineCount() {
			endWrapRow = wrap.bufferLineToFirstWrapRow(hideEndLine + 1)
		}
		if startWrapRow < endWrapRow {
			hidden = append(hidden, [2]int{startWrapRow, endWrapRow})
		}
	}
	m.setHiddenRows(wrapRowCount, hidden)
	m.needsRebuild = false
}

// setHiddenRows installs the projection, merging overlapping and adjacent
// ranges (U:input/editor/display_map/fold_map.rs:346-375).
func (m *foldMap) setHiddenRows(wrapRowCount int, ranges [][2]int) {
	slices.SortStableFunc(ranges, func(a, b [2]int) int { return a[0] - b[0] })
	m.hidden = nil
	m.totalHidden = 0
	m.projectedWrapRowCount = wrapRowCount
	for _, r := range ranges {
		start, end := r[0], min(r[1], wrapRowCount)
		if start >= end {
			continue
		}
		if n := len(m.hidden); n > 0 && start <= m.hidden[n-1].end {
			// Overlapping or adjacent: merge.
			if last := &m.hidden[n-1]; end > last.end {
				m.totalHidden += end - last.end
				last.end = end
			}
			continue
		}
		m.totalHidden += end - start
		m.hidden = append(m.hidden, hiddenRows{start: start, end: end, hiddenBefore: m.totalHidden - (end - start)})
	}
}
