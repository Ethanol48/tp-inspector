package cri

import "testing"

func TestParseClass(t *testing.T) {
	campuses := map[string]bool{"lyn": true, "prs": true, "rns": true, "stg": true, "tls": true}
	cases := []struct {
		slug                string
		campus, promo, code string
		sem                 int
	}{
		{"prepa-sup-s1-lyn-l4", "lyn", "SUP", "L4", 1},
		{"prepa-spe-s3-tls-t1", "tls", "SPE", "T1", 3},
		{"ing-ing1-s6-prs-b10", "prs", "ING1", "B10", 6},
		{"ing-app-ing1-s5-prs-cyb-c1", "prs", "ING1 (apprentices)", "CYB-C1", 5},
		{"ing-ing1-s5-tc-hc-prs-3", "prs", "ING1", "TC-HC-3", 5},
		{"ing-ing1-s6-prs-riemann-a1", "prs", "ING1", "RIEMANN-A1", 6},
		{"cyber-cyb1-s1-rns-r1", "rns", "CYB1", "R1", 1},
		{"prepa-sup-s2s-prs-a1", "prs", "SUP", "A1", 2},
		{"ing-ing1-s5-xyz-b1", "", "ING1", "XYZ-B1", 5},
		{"weird", "", "", "", 0},
	}
	for _, c := range cases {
		got := ParseClass(Group{Slug: c.slug}, campuses)
		if got.Campus != c.campus || got.Promo != c.promo || got.Code != c.code || got.Semester != c.sem {
			t.Errorf("%s: got %q %q %q S%d, want %q %q %q S%d", c.slug,
				got.Campus, got.Promo, got.Code, got.Semester, c.campus, c.promo, c.code, c.sem)
		}
	}
	if c := ParseClass(Group{Slug: "prepa-sup-s2s-prs-a1"}, campuses); c.SemesterLabel() != "S2#" || c.Program != "prepa" {
		t.Errorf("shifted: got %q %q", c.SemesterLabel(), c.Program)
	}
}

func TestSortClasses(t *testing.T) {
	cs := []Class{{Semester: 2, Code: "A1"}, {Semester: 1, Code: "B10"}, {Semester: 1, Code: "B2"}}
	SortClasses(cs)
	if cs[0].Code != "B2" || cs[1].Code != "B10" || cs[2].Code != "A1" {
		t.Errorf("got %v", cs)
	}
}

func TestActivityFilter(t *testing.T) {
	cases := []struct {
		year, bim int
		want      string
	}{
		{2, 6, "S3"}, {2, 5, "S3"}, {2, 7, "S4"}, {1, 2, "S1"}, {1, 4, "S2"},
		{2, 1, "S3–S4"}, // bimester outside the year: both semesters
		{0, 0, ""},
	}
	for _, c := range cases {
		if got := ActivityFilter(c.year, c.bim).Label(); got != c.want {
			t.Errorf("year %d bimester %d: got %q, want %q", c.year, c.bim, got, c.want)
		}
	}
	f := ActivityFilter(2, 6)
	if !f.Match(Class{Semester: 3, Shifted: true}) || f.Match(Class{Semester: 5}) {
		t.Error("S3 filter should keep S3# and drop S5")
	}
}
