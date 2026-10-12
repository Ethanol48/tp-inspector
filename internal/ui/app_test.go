package ui_test

import (
	"testing"
	"tp-inspector/internal/data"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/ui"
)

func TestModel_View(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		slug   tp.Slug
		loader *data.Loader
		list   []string
		want   string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ui.New(tt.slug, tt.loader, tt.list)
			got := m.View()
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("View() = %v, want %v", got, tt.want)
			}
		})
	}
}
