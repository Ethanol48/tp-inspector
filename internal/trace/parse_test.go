package trace

import (
	"os"
	"testing"
)

func TestParseSample(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Tests) != 220 || tr.Failed != 2 || tr.Percent != 99 {
		t.Fatalf("got %d tests, %d failed, %d%%", len(tr.Tests), tr.Failed, tr.Percent)
	}
	var found bool
	for _, tc := range tr.Tests {
		if tc.Name == "missing_newline" {
			found = true
			if tc.Passed || tc.Message == "" {
				t.Fatalf("missing_newline should fail with a message: %+v", tc)
			}
		}
	}
	if !found {
		t.Fatal("missing_newline not found")
	}
	if c := Category("Testsuite.Fundamentals.AddressBook.destroy_book"); c != "Fundamentals.AddressBook" {
		t.Fatalf("category = %q", c)
	}
}

func TestIsGlobalCheck(t *testing.T) {
	global := []Test{
		{Class: "Testsuite.Fundamentals.records.Memory Error", Name: "Errors 1"},
		{Class: "Testsuite.Fundamentals.records", Name: "Errors"},
		{Class: "Testsuite.Fundamentals.report", Name: "Error"},
		{Class: "Testsuite.Fundamentals.report.parse", Name: "basic", Message: "Does not compile"},
	}
	for _, g := range global {
		if !IsGlobalCheck(g) {
			t.Errorf("%+v should be a global check", g)
		}
	}
	real := Test{Class: "Testsuite.Fundamentals.records.load", Name: "error_on_missing_file", Message: "expected -1"}
	if IsGlobalCheck(real) {
		t.Errorf("%+v is a real test", real)
	}
}
