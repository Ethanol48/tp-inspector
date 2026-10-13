package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tp-inspector/internal/trace"
)

// treeNode is a node of a trace tree: the test class path split on dots
// ("Testsuite" › "Fundamentals" › "AddressBook" › "insert_contact") with test
// cases as leaves, like the intranet's trace view.
type treeNode struct {
	name     string
	key      string
	depth    int
	parent   *treeNode
	children []*treeNode
	test     *trace.Test // leaves only
	pass     int
	total    int
	expanded bool
}

func (n *treeNode) leaf() bool { return n.test != nil }
func (n *treeNode) ok() bool   { return n.pass == n.total }

// traceTree is a collapsible tree of one trace with a failure panel.
type traceTree struct {
	root    *treeNode
	rows    []*treeNode // visible nodes, flattened
	cursor  int
	offset  int
	visible int
	onlyBad bool
	df      map[string]int // test id -> students failing it
	pop     int            // students with a trace
}

func newTraceTree(tr *trace.Trace, df map[string]int, pop int) *traceTree {
	root := &treeNode{key: "", depth: -1, expanded: true}
	byKey := map[string]*treeNode{"": root}
	for i := range tr.Tests {
		t := &tr.Tests[i]
		if t.Skipped {
			continue
		}
		parent := root
		var key strings.Builder
		for part := range strings.SplitSeq(t.Class, ".") {
			key.WriteString(".")
			key.WriteString(part)
			n := byKey[key.String()]
			if n == nil {
				n = &treeNode{name: part, key: key.String(), depth: parent.depth + 1, parent: parent}
				byKey[key.String()] = n
				parent.children = append(parent.children, n)
			}
			parent = n
		}
		parent.children = append(parent.children, &treeNode{name: t.Name, key: t.ID, depth: parent.depth + 1, parent: parent, test: t})
	}
	var count func(n *treeNode)
	count = func(n *treeNode) {
		if n.leaf() {
			n.total = 1
			if n.test.Passed {
				n.pass = 1
			}
			return
		}
		// Groups sorted by name, test cases kept in trace order.
		sort.SliceStable(n.children, func(i, j int) bool {
			a, b := n.children[i], n.children[j]
			if a.leaf() || b.leaf() {
				return !a.leaf() && b.leaf()
			}
			return a.name < b.name
		})
		for _, c := range n.children {
			count(c)
			n.pass += c.pass
			n.total += c.total
		}
	}
	count(root)
	t := &traceTree{root: root, df: df, pop: pop}
	t.expandFailing(2)
	return t
}

// expandFailing opens failing groups down to maxDepth and closes the rest.
func (t *traceTree) expandFailing(maxDepth int) {
	var walk func(n *treeNode)
	walk = func(n *treeNode) {
		for _, c := range n.children {
			c.expanded = !c.ok() && c.depth < maxDepth
			walk(c)
		}
	}
	walk(t.root)
	t.flatten()
}

func (t *treeNode) setAll(open bool) {
	for _, c := range t.children {
		c.expanded = open && !c.leaf()
		c.setAll(open)
	}
}

func (t *traceTree) flatten() {
	var key string
	if t.cursor < len(t.rows) {
		key = t.rows[t.cursor].key
	}
	t.rows = t.rows[:0]
	var walk func(n *treeNode)
	walk = func(n *treeNode) {
		for _, c := range n.children {
			if t.onlyBad && c.ok() {
				continue
			}
			t.rows = append(t.rows, c)
			if c.expanded {
				walk(c)
			}
		}
	}
	walk(t.root)
	t.cursor = 0
	for i, r := range t.rows {
		if r.key == key {
			t.cursor = i
			break
		}
	}
}

func (t *traceTree) selected() *treeNode {
	if t.cursor < len(t.rows) {
		return t.rows[t.cursor]
	}
	return nil
}

func (t *traceTree) keys() []string {
	return []string{keyHint("enter/←→", "fold"), keyHint("n/N", "next/prev failure"),
		keyHint("F", "failures only"), keyHint("E/C", "expand/collapse all")}
}

// handleKey reports whether the key was used.
func (t *traceTree) handleKey(k string) bool {
	sel := t.selected()
	switch k {
	case "up", "k":
		t.cursor--
	case "down", "j":
		t.cursor++
	case "pgup", "ctrl+u":
		t.cursor -= max(1, t.visible-1)
	case "pgdown", "ctrl+d":
		t.cursor += max(1, t.visible-1)
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = len(t.rows) - 1
	case "enter", " ":
		if sel != nil && !sel.leaf() {
			sel.expanded = !sel.expanded
			t.flatten()
		}
	case "right", "l":
		if sel != nil && !sel.leaf() {
			if !sel.expanded {
				sel.expanded = true
				t.flatten()
			} else {
				t.cursor++
			}
		}
	case "left", "h":
		if sel == nil {
			break
		}
		if !sel.leaf() && sel.expanded {
			sel.expanded = false
			t.flatten()
		} else if sel.parent != nil && sel.parent != t.root {
			sel.parent.expanded = false
			t.cursor = t.indexOf(sel.parent)
			t.flatten()
		}
	case "n", "N":
		t.jumpFailure(k == "n")
	case "F":
		t.onlyBad = !t.onlyBad
		t.flatten()
	case "E":
		t.root.setAll(true)
		t.flatten()
	case "C":
		t.root.setAll(false)
		t.flatten()
	default:
		return false
	}
	t.cursor = max(0, min(t.cursor, len(t.rows)-1))
	return true
}

func (t *traceTree) indexOf(n *treeNode) int {
	for i, r := range t.rows {
		if r == n {
			return i
		}
	}
	return t.cursor
}

// jumpFailure moves to the next/previous failed test, opening its ancestors.
func (t *traceTree) jumpFailure(next bool) {
	var leaves []*treeNode
	var walk func(n *treeNode)
	walk = func(n *treeNode) {
		for _, c := range n.children {
			if c.leaf() {
				if !c.test.Passed {
					leaves = append(leaves, c)
				}
			} else {
				walk(c)
			}
		}
	}
	walk(t.root)
	if len(leaves) == 0 {
		return
	}
	cur := t.selected()
	pos := -1
	for i, l := range leaves {
		if l == cur {
			pos = i
		}
	}
	var target *treeNode
	switch {
	case pos < 0 && next:
		// First failure after the cursor in display order: fall back to the first one.
		target = leaves[0]
		for _, l := range leaves {
			if idx := t.indexOf(l); idx != t.cursor && idx > t.cursor {
				target = l
				break
			}
		}
	case pos < 0:
		target = leaves[len(leaves)-1]
	case next:
		target = leaves[(pos+1)%len(leaves)]
	default:
		target = leaves[(pos+len(leaves)-1)%len(leaves)]
	}
	for p := target.parent; p != nil && p != t.root; p = p.parent {
		p.expanded = true
	}
	t.flatten()
	t.cursor = t.indexOf(target)
}

func check(ok bool) string {
	if ok {
		return sGood.Render("✓")
	}
	return sBad.Render("✗")
}

// guide draws the tree guides for a row, like "│ │ ├ ".
func guide(n *treeNode) string {
	var parts []string
	for p := n.parent; p != nil && p.parent != nil; p = p.parent {
		parts = append([]string{"│ "}, parts...)
	}
	return lipgloss.NewStyle().Foreground(cFaint).Render(strings.Join(parts, ""))
}

func (t *traceTree) row(n *treeNode) string {
	g := guide(n)
	if n.leaf() {
		name := sDim.Render(n.name)
		if !n.test.Passed {
			name = n.name
		}
		return g + "  " + name + " " + check(n.test.Passed)
	}
	chev := sDim.Render("▸ ")
	if n.expanded {
		chev = sDim.Render("▾ ")
	}
	return g + chev + sBold.Render(n.name) + " " + check(n.ok()) + sDim.Render(fmt.Sprintf(" - %d / %d", n.pass, n.total))
}

func (t *traceTree) treeView(w, h int) string {
	t.visible = max(1, h)
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.visible {
		t.offset = t.cursor - t.visible + 1
	}
	var out []string
	for i := t.offset; i < min(len(t.rows), t.offset+t.visible); i++ {
		prefix := "  "
		if i == t.cursor {
			prefix = sAccent.Bold(true).Render("❯ ")
		}
		out = append(out, ansi.Truncate(prefix+t.row(t.rows[i]), w, "…"))
	}
	if len(out) == 0 {
		out = append(out, sDim.Render("  No test failed."))
	}
	return strings.Join(out, "\n")
}

// failuresView describes the selection: a test's output, or the failed tests
// below a group.
func (t *traceTree) failuresView(w, h int) string {
	sel := t.selected()
	var lines []string
	add := func(s string) {
		for _, l := range strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n") {
			lines = append(lines, strings.TrimRight(l, " "))
		}
	}
	title := sSectionHdr.Render("Failures")
	lines = append(lines, title, lipgloss.NewStyle().Foreground(cFaint).Render(strings.Repeat("─", w)))
	if sel == nil {
		return strings.Join(lines, "\n")
	}
	if sel.leaf() {
		tc := sel.test
		add(sBold.Render(tc.Name) + " " + check(tc.Passed))
		add(sDim.Render(tc.Class))
		if df, ok := t.df[tc.ID]; ok && t.pop > 0 {
			add(sDim.Render(fmt.Sprintf("failed by %d of %d students (%d%%)", df, t.pop, df*100/t.pop)))
		}
		lines = append(lines, "")
		if tc.Passed {
			add(sGood.Render("Passed."))
		} else if tc.Message == "" {
			add(sDim.Render("(no output)"))
		} else {
			add(tc.Message)
		}
	} else {
		failed := 0
		var walk func(n *treeNode)
		walk = func(n *treeNode) {
			for _, c := range n.children {
				if !c.leaf() {
					walk(c)
					continue
				}
				if c.test.Passed {
					continue
				}
				failed++
				if len(lines) < h-1 {
					lines = append(lines, "")
					add(sBad.Render("✗ ") + c.name + sDim.Render(fmt.Sprintf("  failed by %d", t.df[c.test.ID])))
					if c.test.Message != "" {
						add(sDim.Render(c.test.Message))
					}
				}
			}
		}
		add(sBold.Render(strings.TrimPrefix(sel.key, ".")) + sDim.Render(fmt.Sprintf(" - %d / %d", sel.pass, sel.total)))
		walk(sel)
		if failed == 0 {
			lines = append(lines, "", sGood.Render("Every test passes."))
		}
	}
	if len(lines) > h {
		lines = append(lines[:h-1], sDim.Render("…"))
	}
	return strings.Join(lines, "\n")
}

// View renders the header, the tree and the failure panel in w×h.
func (t *traceTree) View(pct int, w, h int) string {
	passed, total := t.root.pass, t.root.total
	if pct < 0 && total > 0 {
		pct = passed * 100 / total
	}
	head := pctStyle(pct).Bold(true).Render(fmt.Sprintf("%d%%", pct)) + " " + check(passed == total) +
		"  " + sDim.Render(fmt.Sprintf("%d/%d tests passed", passed, total)) + "  " +
		bar(float64(passed)/float64(max(1, total)), min(40, max(10, w/4)), pctStyle(pct))
	if t.onlyBad {
		head += sWarn.Render("  · failures only")
	}
	bodyH := max(3, h-2)
	if w >= 110 {
		treeW := w * 55 / 100
		left := lipgloss.NewStyle().Width(treeW).Render(t.treeView(treeW, bodyH))
		right := t.failuresView(w-treeW-3, bodyH)
		return head + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
	}
	panelH := min(10, bodyH/2)
	return head + "\n\n" + t.treeView(w, bodyH-panelH-1) + "\n\n" + t.failuresView(w, panelH-1)
}
