package ui

import "testing"

func TestMeanMedian(t *testing.T) {
	cases := []struct {
		in           []int
		mean, median float64
	}{
		{nil, 0, 0},
		{[]int{40}, 40, 40},
		{[]int{10, 20, 90}, 40, 20},
		{[]int{0, 50, 60, 100}, 52.5, 55},
	}
	for _, c := range cases {
		mean, median := meanMedian(c.in)
		if mean != c.mean || median != c.median {
			t.Errorf("%v: got %v/%v, want %v/%v", c.in, mean, median, c.mean, c.median)
		}
	}
}
