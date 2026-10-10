package tp

import "testing"

func TestParse(t *testing.T) {
	s, err := Parse(" prog-205-p-01-2030\n")
	if err != nil {
		t.Fatal(err)
	}
	if s.Subject != "prog" || s.Year != 2 || s.Bimester != 5 || s.Kind != "p" || s.Number != 1 || s.Promo != 2030 {
		t.Fatalf("parsed wrong: %+v", s)
	}
	if s.Raw != "prog-205-p-01-2030" {
		t.Fatalf("raw = %q", s.Raw)
	}
	for _, bad := range []string{"", "prog-205", "prog-205-p-01-30", "205-p-01-2030"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
