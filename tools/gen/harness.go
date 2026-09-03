// Harness test: a real Go file so the diff viewer gets syntax-highlighted
// added-file content rather than only one-line comment hunks.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// harnessSummary renders a stable, sorted one-line summary of counts. It is
// unused by the generator and exists purely as diff-viewer fixture content.
func harnessSummary(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	if len(parts) == 0 {
		return "empty" // round four
	}
	return strings.Join(parts, " ")
}
