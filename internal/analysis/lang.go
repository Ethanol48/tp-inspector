package analysis

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lang is a source language the code analysis understands.
type Lang int

const (
	LangNone Lang = iota
	LangC
	LangPython
)

// LangOf returns the language of a file from its extension.
func LangOf(path string) Lang {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".h":
		return LangC
	case ".py":
		return LangPython
	}
	return LangNone
}

// tokKind classifies lexer tokens.
type tokKind int

const (
	tIdent tokKind = iota
	tKeyword
	tNumber
	tString
	tPunct
	tComment
	tPreproc
)

type token struct {
	kind tokKind
	text string
	line int // 1-based
	// Python only: indentation of the line the token starts, -1 when the
	// token does not start a logical line.
	indent int
}

var cKeywords = set("auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while _Bool bool size_t ssize_t uint8_t uint16_t uint32_t uint64_t int8_t int16_t int32_t int64_t NULL true false")

var pyKeywords = set("False None True and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield match case self")

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// C punctuators, longest first.
var cPuncts = []string{"<<=", ">>=", "...", "->", "++", "--", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "&=", "^=", "|="}

var pyPuncts = []string{"**=", "//=", ">>=", "<<=", "->", ":=", "**", "//", "<<", ">>", "<=", ">=", "==", "!=",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "@="}

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentPart(r rune) bool  { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// codeLineRes matches a comment line that looks like commented-out code
// ("print(overload(3))", "x = f(a);", "}", "if (a < b)", "import os"…).
var codeLineRes = []*regexp.Regexp{
	// ends like a statement or a block: "x = f(a);", "{", "}"
	regexp.MustCompile(`[;{}]$`),
	// call: "print(overload(3))", "obj.run(a, b)", "p->next(x);"
	regexp.MustCompile(`^[A-Za-z_][\w.>\[\]-]*\(.*\)[;:]?$`),
	// C control flow
	regexp.MustCompile(`^(else\s+)?(if|while|for|switch)\s*\(.*\)$`),
	regexp.MustCompile(`^else$`),
	// assignment: "x = 3", "a[i] += f(b)"; the right side is one token or
	// contains a digit, quote or bracket, so that prose "x = the sum" is kept
	regexp.MustCompile(`^[A-Za-z_][\w.\[\]>*-]*\s*([-+*/%|&^]|<<|>>)?=\s*([^\s=]\S*|.*[0-9"'()\[\]{}].*)$`),
	// preprocessor
	regexp.MustCompile(`^#\s*(include\s*[<"]|define\s+\w|ifn?def\s+\w|endif|pragma\s)`),
	// Python
	regexp.MustCompile(`^(def|class)\s+\w+.*:$`),
	regexp.MustCompile(`^(else|try|finally)\s*:$`),
	regexp.MustCompile(`^(if|elif|while|for|with|except)\b.*[()=<>!\[\]].*:$`),
	regexp.MustCompile(`^(import\s+\w+|from\s+[\w.]+\s+import\s+\S+)`),
	regexp.MustCompile(`^(pass|break|continue|return)$`),
}

// commentTextLines returns the lines of a comment token without its markers
// (//, /*, */, #, leading *).
func commentTextLines(text string) []string {
	var out []string
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if i == 0 {
			l = strings.TrimLeft(l, "/*#")
		} else {
			l = strings.TrimPrefix(l, "*")
		}
		l = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l), "*/"))
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// isCommentedCode reports a comment that is code put aside, like
// "//print(overload(3))": at least half of its lines look like code.
// Doc comments (/** … */) are never commented-out code.
func isCommentedCode(text string) bool {
	if strings.HasPrefix(text, "/**") {
		return false
	}
	lines := commentTextLines(text)
	code := 0
	for _, l := range lines {
		for _, re := range codeLineRes {
			if re.MatchString(l) {
				code++
				break
			}
		}
	}
	return len(lines) > 0 && 2*code >= len(lines)
}

// dropCommentedCode removes the comments that hold commented-out code: they
// are neither comments nor code of the student.
func dropCommentedCode(toks []token) []token {
	return slices.DeleteFunc(toks, func(t token) bool {
		return t.kind == tComment && isCommentedCode(t.text)
	})
}

// lexC splits C source into tokens; comments and preprocessor lines are kept
// as single tokens so callers can use or drop them. Commented-out code is
// dropped.
func lexC(src string) []token {
	var out []token
	line, i, atLineStart := 1, 0, true
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
			atLineStart = true
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '#' && atLineStart:
			start, l := i, line
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\\' && i+1 < len(src) && src[i+1] == '\n' {
					i += 2
					line++
					continue
				}
				i++
			}
			out = append(out, token{tPreproc, src[start:i], l, -1})
			continue
		}
		atLineStart = false
		start, l := i, line
		switch {
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out = append(out, token{tComment, src[start:i], l, -1})
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
			} else {
				i += 2 + end + 2
			}
			line += strings.Count(src[start:i], "\n")
			out = append(out, token{tComment, src[start:i], l, -1})
		case c == '"' || c == '\'':
			i++
			for i < len(src) && src[i] != c && src[i] != '\n' {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i = min(i+1, len(src))
			out = append(out, token{tString, src[start:i], l, -1})
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			for i < len(src) && (isIdentPart(rune(src[i])) || src[i] == '.') {
				i++
			}
			out = append(out, token{tNumber, src[start:i], l, -1})
		default:
			r, size := utf8.DecodeRuneInString(src[i:])
			if isIdentStart(r) {
				for i < len(src) {
					r, size := utf8.DecodeRuneInString(src[i:])
					if !isIdentPart(r) {
						break
					}
					i += size
				}
				w := src[start:i]
				k := tIdent
				if cKeywords[w] {
					k = tKeyword
				}
				out = append(out, token{k, w, l, -1})
				continue
			}
			p := string(r)
			for _, cand := range cPuncts {
				if strings.HasPrefix(src[i:], cand) {
					p, size = cand, len(cand)
					break
				}
			}
			i += size
			out = append(out, token{tPunct, p, l, -1})
		}
	}
	return dropCommentedCode(out)
}

// lexPython splits Python source into tokens. The first token of each
// logical line carries its indentation. Commented-out code is dropped.
func lexPython(src string) []token {
	var out []token
	line, i := 1, 0
	depth := 0 // brackets: newlines inside them do not end the logical line
	lineStart := true
	indent := 0
	for i < len(src) {
		c := src[i]
		if lineStart {
			indent = 0
			for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
				if src[i] == '\t' {
					indent += 8 - indent%8
				} else {
					indent++
				}
				i++
			}
			if i < len(src) && (src[i] == '\n' || src[i] == '\r') {
				// blank line
				if src[i] == '\n' {
					line++
				}
				i++
				continue
			}
			if i >= len(src) {
				break
			}
			c = src[i]
		}
		tokIndent := -1
		if lineStart && depth == 0 && c != '#' {
			tokIndent = indent
		}
		switch {
		case c == '\n':
			line++
			i++
			if depth == 0 {
				lineStart = true
			}
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			i++
			continue
		case c == '\\' && i+1 < len(src) && src[i+1] == '\n':
			i += 2
			line++
			continue
		}
		if c != '#' {
			lineStart = false
		}
		start, l := i, line
		switch {
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out = append(out, token{tComment, src[start:i], l, -1})
		case isPyStringStart(src[i:]):
			j := i
			for j < len(src) && src[j] != '"' && src[j] != '\'' {
				j++
			}
			q := src[j : j+1]
			if strings.HasPrefix(src[j:], q+q+q) {
				q = q + q + q
			}
			i = j + len(q)
			for i < len(src) && !strings.HasPrefix(src[i:], q) {
				if src[i] == '\\' {
					i++
				} else if src[i] == '\n' && len(q) == 1 {
					break
				}
				i++
			}
			i = min(i+len(q), len(src))
			line += strings.Count(src[start:i], "\n")
			out = append(out, token{tString, src[start:i], l, tokIndent})
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			for i < len(src) && (isIdentPart(rune(src[i])) || src[i] == '.') {
				i++
			}
			out = append(out, token{tNumber, src[start:i], l, tokIndent})
		default:
			r, size := utf8.DecodeRuneInString(src[i:])
			if isIdentStart(r) {
				for i < len(src) {
					r, size := utf8.DecodeRuneInString(src[i:])
					if !isIdentPart(r) {
						break
					}
					i += size
				}
				w := src[start:i]
				k := tIdent
				if pyKeywords[w] {
					k = tKeyword
				}
				out = append(out, token{k, w, l, tokIndent})
				continue
			}
			p := string(r)
			for _, cand := range pyPuncts {
				if strings.HasPrefix(src[i:], cand) {
					p, size = cand, len(cand)
					break
				}
			}
			switch p {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth = max(0, depth-1)
			}
			i += size
			out = append(out, token{tPunct, p, l, tokIndent})
		}
	}
	return dropCommentedCode(out)
}

// isPyStringStart reports whether s starts a string literal, prefixes included.
func isPyStringStart(s string) bool {
	j := 0
	for j < len(s) && j < 3 && strings.ContainsRune("rRbBuUfF", rune(s[j])) {
		j++
	}
	return j < len(s) && (s[j] == '"' || s[j] == '\'')
}

// Func is one function of a repo.
type Func struct {
	File       string // relative to the repo
	Name       string
	Start, End int // lines, 1-based, inclusive
	Lang       Lang
	Raw        string // source lines Start..End
	// Norm and Exact cover the body only: the subject gives every student the
	// same name and parameters, only bodies tell a copy apart.
	Norm     []string // normalized body tokens (identifiers I, numbers N, strings S)
	Exact    []string // body tokens as written: only whitespace and comments differ between copies
	Idents   []string // raw identifiers
	Calls    []string // called names
	Comments []string // comments and docstrings, including the one just above
	Doc      string   // doc comment / docstring, if any
	Body     []token  // code tokens of the body (no comments)
}

// normalize maps a code token to its normalized form.
func normalize(t token) string {
	switch t.kind {
	case tIdent:
		return "I"
	case tNumber:
		return "N"
	case tString:
		return "S"
	}
	return t.text
}

// ExtractFuncs returns the functions of one source file.
func ExtractFuncs(file, src string) []Func {
	switch LangOf(file) {
	case LangC:
		return extractC(file, src)
	case LangPython:
		return extractPython(file, src)
	}
	return nil
}

func sourceLines(lines []string, start, end int) string {
	start, end = max(1, start), min(len(lines), end)
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

func extractC(file, src string) []Func {
	toks := lexC(src)
	lines := strings.Split(src, "\n")
	var out []Func
	depth := 0
	lastTop := -1 // index of the last token ending a top-level declaration
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.kind == tPreproc {
			if depth == 0 {
				lastTop = i
			}
			continue
		}
		if t.kind != tPunct {
			continue
		}
		switch t.text {
		case "{":
			if depth == 0 {
				if f, end, ok := cFunc(file, toks, lines, lastTop, i); ok {
					out = append(out, f)
					i = end
					lastTop = end
					continue
				}
			}
			depth++
		case "}":
			depth = max(0, depth-1)
			if depth == 0 {
				lastTop = i
			}
		case ";":
			if depth == 0 {
				lastTop = i
			}
		}
	}
	return out
}

// cFunc recognizes "type name(params) {" ending at toks[open] and returns the
// function up to its matching brace.
func cFunc(file string, toks []token, lines []string, lastTop, open int) (Func, int, bool) {
	// Previous code token must be ')'.
	j := open - 1
	for j > lastTop && toks[j].kind == tComment {
		j--
	}
	if j <= lastTop || toks[j].text != ")" {
		return Func{}, 0, false
	}
	d := 0
	for ; j > lastTop; j-- {
		if toks[j].kind != tPunct {
			continue
		}
		if toks[j].text == ")" {
			d++
		} else if toks[j].text == "(" {
			d--
			if d == 0 {
				break
			}
		}
	}
	if j <= lastTop+0 || j-1 <= lastTop || toks[j-1].kind != tIdent {
		return Func{}, 0, false
	}
	name := toks[j-1].text
	// Matching closing brace.
	end, d := open, 0
	for ; end < len(toks); end++ {
		if toks[end].kind != tPunct {
			continue
		}
		if toks[end].text == "{" {
			d++
		} else if toks[end].text == "}" {
			d--
			if d == 0 {
				break
			}
		}
	}
	if end >= len(toks) {
		end = len(toks) - 1
	}
	// Header starts after the last top-level declaration; comments there are
	// the function's doc.
	first := lastTop + 1
	f := Func{File: file, Name: name, Lang: LangC, End: toks[end].line}
	for k := first; k <= end; k++ {
		t := toks[k]
		if t.kind == tComment {
			f.Comments = append(f.Comments, t.text)
			if k < open && f.Doc == "" && strings.HasPrefix(t.text, "/**") {
				f.Doc = t.text
			}
			continue
		}
		if t.kind == tPreproc {
			continue
		}
		if f.Start == 0 {
			f.Start = t.line
		}
		if t.kind == tIdent {
			f.Idents = append(f.Idents, t.text)
			if k+1 <= end && toks[k+1].text == "(" {
				f.Calls = append(f.Calls, t.text)
			}
		}
		if k > open {
			f.Body = append(f.Body, t)
			f.Norm = append(f.Norm, normalize(t))
			f.Exact = append(f.Exact, t.text)
		}
	}
	// The doc comment just above belongs in Raw too.
	rawStart := f.Start
	for k := first; k < open; k++ {
		if toks[k].kind == tComment {
			rawStart = min(rawStart, toks[k].line)
			break
		}
	}
	f.Raw = sourceLines(lines, rawStart, f.End)
	return f, end, true
}

func extractPython(file, src string) []Func {
	toks := lexPython(src)
	lines := strings.Split(src, "\n")
	var out []Func
	type open struct {
		indent int
		isDef  bool
	}
	var stack []open // enclosing blocks that matter: class and def
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.indent < 0 {
			continue
		}
		for len(stack) > 0 && t.indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		isDef := t.text == "def" || t.text == "async" && i+1 < len(toks) && toks[i+1].text == "def"
		if t.text == "class" {
			stack = append(stack, open{t.indent, false})
			continue
		}
		if !isDef {
			continue
		}
		insideDef := false
		for _, o := range stack {
			if o.isDef {
				insideDef = true
			}
		}
		stack = append(stack, open{t.indent, true})
		if insideDef {
			continue // nested function: part of its parent
		}
		// The function ends before the next logical line indented ≤ def.
		end := i + 1
		for end < len(toks) && !(toks[end].indent >= 0 && toks[end].indent <= t.indent) {
			end++
		}
		// Decorators and comments right above.
		start := i
		for start > 0 {
			p := start - 1
			ln := toks[p].line
			if ln < toks[start].line-1 {
				break
			}
			for p > 0 && toks[p-1].line == ln {
				p--
			}
			if toks[p].kind != tComment && !(toks[p].text == "@" && toks[p].indent == t.indent) {
				break
			}
			start = p
		}
		out = append(out, pyFunc(file, toks[start:end], lines, t))
	}
	return out
}

func pyFunc(file string, toks []token, lines []string, def token) Func {
	f := Func{File: file, Lang: LangPython, Start: toks[0].line, End: def.line}
	nameAt := -1
	for k, t := range toks {
		if t.text == "def" && k+1 < len(toks) {
			nameAt = k + 1
			f.Name = toks[k+1].text
			break
		}
	}
	// The docstring is the first statement after the header's ':'.
	bodyAt := -1
	if nameAt >= 0 {
		d := 0
		for k := nameAt; k < len(toks); k++ {
			switch toks[k].text {
			case "(", "[", "{":
				d++
			case ")", "]", "}":
				d--
			case ":":
				if d == 0 && bodyAt < 0 {
					bodyAt = k + 1
				}
			}
			if bodyAt >= 0 {
				break
			}
		}
	}
	for k, t := range toks {
		f.End = max(f.End, t.line+strings.Count(t.text, "\n"))
		if t.kind == tComment {
			f.Comments = append(f.Comments, t.text)
			continue
		}
		if k == bodyAt && t.kind == tString && (k+1 == len(toks) || toks[k+1].indent >= 0 || toks[k+1].line > t.line) {
			f.Doc = t.text
			f.Comments = append(f.Comments, t.text)
			continue
		}
		if t.kind == tIdent {
			f.Idents = append(f.Idents, t.text)
			if k+1 < len(toks) && toks[k+1].text == "(" {
				f.Calls = append(f.Calls, t.text)
			}
		}
		if bodyAt >= 0 && k >= bodyAt {
			f.Body = append(f.Body, t)
			f.Norm = append(f.Norm, normalize(t))
			f.Exact = append(f.Exact, t.text)
		}
	}
	f.Raw = sourceLines(lines, f.Start, f.End)
	return f
}