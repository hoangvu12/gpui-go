package gpui

// The selection band test of the inline text renderer (U:text/inline.rs:
// 1321-1362, pointInTextSelection): whether the character at pos, given
// its advance, lies inside the band a selection's two endpoints describe.
// Moved from base into the core with the selection model (browser-core
// ticket 19) — the model's plain-run projection and the inline renderer's
// own painting share the one rule; Base keeps an unexported wrapper.

// PointInTextSelection reports whether pos is inside the selection band,
// considering multi-line selections (U:text/inline.rs:1321-1362): on the
// rows between the endpoints everything is selected, on the first and
// last rows up to (and one half-advance past) the endpoint, and a
// character is in when its midpoint is.
func PointInTextSelection(pos Point, charWidth float32, selectionStart, selectionEnd Point, lineHeight float32) bool {
	pointInLine := func(p Point) bool { return p.Y >= pos.Y && p.Y < pos.Y+lineHeight }
	top, bottom := min(selectionStart.Y, selectionEnd.Y), max(selectionStart.Y, selectionEnd.Y)
	x := pos.X + charWidth/2
	if pos.Y+lineHeight <= top || pos.Y > bottom {
		return false
	}
	if pointInLine(selectionStart) && pointInLine(selectionEnd) {
		left, right := min(selectionStart.X, selectionEnd.X), max(selectionStart.X, selectionEnd.X)
		return x >= left && x <= right
	}
	topPoint, bottomPoint := selectionEnd, selectionStart
	if selectionStart.Y < selectionEnd.Y {
		topPoint, bottomPoint = selectionStart, selectionEnd
	}
	switch {
	case pointInLine(topPoint):
		return x >= topPoint.X
	case pointInLine(bottomPoint):
		return x <= bottomPoint.X
	}
	return true
}

// pointInTextSelection is the internal spelling.
func pointInTextSelection(pos Point, charWidth float32, selectionStart, selectionEnd Point, lineHeight float32) bool {
	return PointInTextSelection(pos, charWidth, selectionStart, selectionEnd, lineHeight)
}

// pointAdd is Point + Point (the text renderer's helper).
func pointAdd(a, b Point) Point { return Point{X: a.X + b.X, Y: a.Y + b.Y} }

// pointSub is Point - Point.
func pointSub(a, b Point) Point { return Point{X: a.X - b.X, Y: a.Y - b.Y} }
