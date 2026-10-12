// Package analysis finds suspicious students: risk from the submission
// metadata, code shared between repos and code that looks AI-generated.
package analysis

import (
	"fmt"
	"math"
)

// Level is a coarse, readable reading of a 0..1 score.
type Level int

const (
	Low Level = iota
	Medium
	High
)

func (l Level) String() string {
	switch l {
	case High:
		return "high"
	case Medium:
		return "medium"
	}
	return "low"
}

// LevelOf maps a score to a level: high from 0.8, medium from 0.5.
func LevelOf(score float64) Level {
	switch {
	case score >= 0.8:
		return High
	case score >= 0.5:
		return Medium
	}
	return Low
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

// squash maps 0..∞ to 0..1, reaching ~63% at scale.
func squash(x, scale float64) float64 {
	if x <= 0 {
		return 0
	}
	return 1 - math.Exp(-x/scale)
}
