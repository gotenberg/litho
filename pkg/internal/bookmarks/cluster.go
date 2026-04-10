package bookmarks

// clusterSizesToLevels groups heading font sizes into heading levels.
// Input: sizes sorted descending (largest first). Output: size → level map,
// where level 1 is the largest. Sizes within mergeTolerance of each other
// are collapsed into the same level. Levels beyond maxDepth are capped.
func clusterSizesToLevels(sizes []float64, mergeTolerance float64, maxDepth int) map[float64]int {
	result := make(map[float64]int, len(sizes))
	if len(sizes) == 0 {
		return result
	}
	if maxDepth < 1 {
		maxDepth = 1
	}

	level := 1
	currentBase := sizes[0]
	result[sizes[0]] = level

	for i := 1; i < len(sizes); i++ {
		s := sizes[i]
		// If within tolerance of the current base, same level.
		if currentBase-s <= mergeTolerance*currentBase {
			result[s] = level
			continue
		}
		// Different level.
		if level < maxDepth {
			level++
		}
		currentBase = s
		result[s] = level
	}

	return result
}
