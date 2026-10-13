// Package trace parses Forge test traces (JUnit XML).
package trace

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

// Test is one test case of a trace.
type Test struct {
	ID      string // Class + "::" + Name
	Class   string // e.g. "Testsuite.Fundamentals.AddressBook.destroy_book"
	Name    string
	Passed  bool
	Skipped bool
	Message string // normalized failure output, empty if passed
}

// Trace is a parsed trace.xml.
type Trace struct {
	Percent int // successPercentOverride, -1 if absent
	Tests   []Test
	Failed  int
}

type xmlTestsuites struct {
	PercentOverride string         `xml:"successPercentOverride,attr"`
	Suites          []xmlTestsuite `xml:"testsuite"`
	Cases           []xmlTestcase  `xml:"testcase"`
}

type xmlTestsuite struct {
	Cases []xmlTestcase `xml:"testcase"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type xmlTestcase struct {
	Class   string      `xml:"classname,attr"`
	Name    string      `xml:"name,attr"`
	Failure *xmlFailure `xml:"failure"`
	Error   *xmlFailure `xml:"error"`
	Skipped *struct{}   `xml:"skipped"`
}

var (
	addrRe  = regexp.MustCompile(`0x[0-9a-fA-F]{6,}`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// normalize strips run-specific noise (pointer values, whitespace) from
// failure output so identical wrong behaviour yields identical messages.
func normalize(s string) string {
	s = addrRe.ReplaceAllString(s, "0xADDR")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// Parse decodes a trace document.
func Parse(data []byte) (*Trace, error) {
	var doc xmlTestsuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	t := &Trace{Percent: -1}
	if p, err := strconv.Atoi(doc.PercentOverride); err == nil {
		t.Percent = p
	}
	cases := doc.Cases
	for _, s := range doc.Suites {
		cases = append(cases, s.Cases...)
	}
	for _, c := range cases {
		tc := Test{ID: c.Class + "::" + c.Name, Class: c.Class, Name: c.Name, Passed: true}
		fail := c.Failure
		if fail == nil {
			fail = c.Error
		}
		switch {
		case fail != nil:
			tc.Passed = false
			tc.Message = normalize(fail.Text)
			if tc.Message == "" {
				tc.Message = normalize(fail.Message)
			}
			t.Failed++
		case c.Skipped != nil:
			tc.Passed = false
			tc.Skipped = true
		}
		t.Tests = append(t.Tests, tc)
	}
	return t, nil
}

// Category returns a short grouping label for a test class:
// "Testsuite.Fundamentals.AddressBook.destroy_book" -> "Fundamentals.AddressBook".
func Category(class string) string {
	class = strings.TrimPrefix(class, "Testsuite.")
	parts := strings.Split(class, ".")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, ".")
}

var errorsNameRe = regexp.MustCompile(`^Errors?( \d+)?$`)

// IsGlobalCheck reports whether a test is a per-exercise global check rather
// than a real test case: memory-error counters ("Memory Error › Errors 3"),
// build error checks ("records › Errors") and "Does not compile" failures.
// Comparing them across the whole promo makes no sense, but they are still
// worth seeing when comparing two students.
func IsGlobalCheck(t Test) bool {
	return strings.HasSuffix(t.Class, "Memory Error") ||
		errorsNameRe.MatchString(t.Name) ||
		isCompileFailure(t.Message)
}

func isCompileFailure(msg string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(msg), "."), "does not compile")
}
