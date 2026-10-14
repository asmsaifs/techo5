package display

// clipRunes is the longest start of s, of at least least letters, that fits with an ellipsis after it,
// with the ellipsis: fits says whether a line fits. Found by halving rather than by dropping a letter
// at a time, which measured the whole line again for every letter and took seconds on a name
// thousands of letters long (a Home Assistant name has no length limit). s is a line that does not
// fit whole.
func clipRunes(s string, least int, fits func(string) bool) string {
	rs := []rune(s)
	least = max(0, min(least, len(rs)))
	lo, hi := least, len(rs)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(string(rs[:mid]) + "…") {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(rs[:lo]) + "…"
}
