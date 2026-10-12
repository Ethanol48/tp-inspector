// Package tp parses EPITA activity slugs such as "prog-205-p-01-2030".
package tp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var slugRe = regexp.MustCompile(`^([a-z]+)-(\d)(\d+)-([a-z]+)-(\d+)-(\d{4})$`)

// Slug is a parsed activity slug.
type Slug struct {
	Raw      string
	Subject  string // "prog"
	Year     int    // 2 -> second year
	Bimester int    // 5
	Kind     string // "p" -> TP / assignment
	Number   int    // 1
	Promo    int    // 2030
}

// Parse validates and decomposes an activity slug.
func Parse(s string) (Slug, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	m := slugRe.FindStringSubmatch(s)
	if m == nil {
		return Slug{}, fmt.Errorf("invalid assignment slug %q (expected e.g. prog-205-p-01-2030)", s)
	}
	year, _ := strconv.Atoi(m[2])
	bim, _ := strconv.Atoi(m[3])
	num, _ := strconv.Atoi(m[5])
	promo, _ := strconv.Atoi(m[6])
	return Slug{Raw: s, Subject: m[1], Year: year, Bimester: bim, Kind: m[4], Number: num, Promo: promo}, nil
}

// KindName returns a human name for the kind letter.
func (s Slug) KindName() string {
	switch s.Kind {
	case "p":
		return "TP"
	case "e":
		return "Exam"
	case "r":
		return "Retake"
	case "t":
		return "Tutorial"
	}
	return strings.ToUpper(s.Kind)
}

// Describe returns a one-line human description.
func (s Slug) Describe() string {
	return fmt.Sprintf("%s · %s %d · year %d · bimester %d · promo %d",
		strings.ToUpper(s.Subject), s.KindName(), s.Number, s.Year, s.Bimester, s.Promo)
}
