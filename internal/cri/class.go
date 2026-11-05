package cri

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Class is a class group split from its slug, which reads
// <program>-<promo>-<semester>[-<track>…]-<campus>-<class>:
//
//	prepa-sup-s1-lyn-l4          SUP   S1  Lyon  L4
//	ing-app-ing1-s5-prs-cyb-c1   ING1 (apprentices)  S5  Paris  CYB-C1
//	ing-ing1-s5-tc-hc-prs-3      ING1  S5  Paris  TC-HC-3
//	prepa-sup-s2s-prs-a1         SUP   S2# (shifted)  Paris  A1
type Class struct {
	Group
	Program  string // prepa, ing, ing-app, cyber…
	Campus   string // campus slug, "" when not found
	Promo    string // SUP, SPE, ING1, CYB1…; "" when the slug does not parse
	Semester int    // 0 when unknown
	Shifted  bool   // shifted semester ("S2#")
	Code     string // B1, CYB-C1…
}

// SemesterLabel is S1, S2#…, or "" when unknown.
func (c Class) SemesterLabel() string {
	if c.Semester == 0 {
		return ""
	}
	s := "S" + strconv.Itoa(c.Semester)
	if c.Shifted {
		s += "#"
	}
	return s
}

var semRe = regexp.MustCompile(`^s(\d+)(s?)$`)

// ParseClass splits a class slug; campuses are the known campus slugs.
func ParseClass(g Group, campuses map[string]bool) Class {
	c := Class{Group: g}
	parts := strings.Split(g.Slug, "-")
	sem := -1
	for i, p := range parts {
		if m := semRe.FindStringSubmatch(p); m != nil && i > 0 {
			sem = i
			c.Semester, _ = strconv.Atoi(m[1])
			c.Shifted = m[2] != ""
			break
		}
	}
	if sem < 0 {
		return c
	}
	c.Program = strings.Join(parts[:sem-1], "-")
	c.Promo = strings.ToUpper(parts[sem-1])
	if sem >= 2 && parts[sem-2] == "app" {
		c.Promo += " (apprentices)"
	}
	camp := -1
	for i := sem + 1; i < len(parts); i++ {
		if campuses[parts[i]] {
			camp = i
			break
		}
	}
	if camp < 0 {
		c.Code = strings.ToUpper(strings.Join(parts[sem+1:], "-"))
		return c
	}
	c.Campus = parts[camp]
	code := append(append([]string{}, parts[sem+1:camp]...), parts[camp+1:]...)
	c.Code = strings.ToUpper(strings.Join(code, "-"))
	return c
}

// SortClasses orders classes by semester, then by code with numbers
// compared as numbers (B2 before B10).
func SortClasses(cs []Class) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Semester != cs[j].Semester {
			return cs[i].Semester < cs[j].Semester
		}
		if cs[i].Shifted != cs[j].Shifted {
			return !cs[i].Shifted
		}
		return naturalLess(cs[i].Code, cs[j].Code)
	})
}

// ProgramRank orders programs as students go through them: prepa, then the
// engineering cycle, apprentices, cyber, then the rest.
func ProgramRank(program string) int {
	switch program {
	case "prepa":
		return 0
	case "ing":
		return 1
	case "ing-app":
		return 2
	case "cyber":
		return 3
	}
	return 4
}

var numRe = regexp.MustCompile(`\d+|\D+`)

func naturalLess(a, b string) bool {
	pa, pb := numRe.FindAllString(a, -1), numRe.FindAllString(b, -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil {
			return na < nb
		}
		return pa[i] < pb[i]
	}
	return len(pa) < len(pb)
}
