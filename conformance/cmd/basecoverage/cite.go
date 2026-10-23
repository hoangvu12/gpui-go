package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// citation is one `U:path[:lines]` reference in a Go file.
type citation struct {
	File    string // repo-relative Go file
	Line    int    // Go line of the citation
	Path    string // upstream path as written (relative to crates/base/src)
	Ranges  [][2]int
	Context string // the enclosing Go func/decl name, or "" for a free comment
	// Deviated reports the citing context records a deviation;
	// NotApplicable that it records the cited span as not ported.
	Deviated      bool
	NotApplicable bool
	Deferred      bool
	Sentence      string // the citing sentence (free comments) for reasons
}

var (
	citeRe     = regexp.MustCompile(`U:([A-Za-z0-9_./-]+?\.rs)((?::[ \t]*(?:\r?\n[ \t]*//[ \t]*)?\d+(?:[ \t]*-[ \t]*\d+)?)(?:(?:[ \t]*,[ \t]*|[ \t]+and[ \t]+)(?:\r?\n[ \t]*//[ \t]*)?\d+(?:[ \t]*-[ \t]*\d+)?)*)?`)
	numRangeRe = regexp.MustCompile(`(\d+)(?:[ \t]*-[ \t]*(\d+))?`)
	deviatedRe = regexp.MustCompile(`(?i)deviat|adapted`)
	naRe       = regexp.MustCompile(`(?i)not ported|not applicable|\bn/a\b|\bn\.a\.|left for|out of scope|cannot be ported`)
	deferredRe = regexp.MustCompile(`(?i)deferred|request_autoscroll|ticket 37|→ ?37|-> ?37`)
)

// goSourceFile is one Go file's text and parsed form.
type goSourceFile struct {
	Rel  string
	Src  string
	Fset *token.FileSet
	AST  *ast.File
}

func parseGo(rel string, src []byte) (*goSourceFile, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &goSourceFile{Rel: rel, Src: string(src), Fset: fset, AST: f}, nil
}

// citedPaths returns the distinct upstream paths a Go file cites.
func citedPaths(src string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range citeRe.FindAllStringSubmatch(src, -1) {
		p := strings.TrimPrefix(m[1], "./")
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// goTestFunc is one func of a Go test file, for name matching.
type goTestFunc struct {
	File     string
	Name     string
	Deviated bool
	Cites    map[string]bool // upstream paths the file cites
}

// scanCitations extracts every citation of a Go test file with its
// context classification, and the file's funcs.
func scanCitations(g *goSourceFile) ([]citation, []goTestFunc) {
	type span struct {
		from, to int
		name     string
		comments string
	}
	tf := g.Fset.File(g.AST.Pos())
	off := func(p token.Pos) int { return tf.Offset(p) }
	var decls []span
	for _, d := range g.AST.Decls {
		var from token.Pos
		name := ""
		switch d := d.(type) {
		case *ast.FuncDecl:
			from = d.Pos()
			if d.Doc != nil {
				from = d.Doc.Pos()
			}
			name = d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = "(method) " + name
			}
		case *ast.GenDecl:
			from = d.Pos()
			if d.Doc != nil {
				from = d.Doc.Pos()
			}
			name = "(decl)"
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.ValueSpec:
					if len(s.Names) > 0 {
						name = s.Names[0].Name
					}
				case *ast.TypeSpec:
					name = s.Name.Name
				}
				break // the first spec names the declaration
			}
		}
		sp := span{from: off(from), to: off(d.End()), name: name}
		var b strings.Builder
		for _, cg := range g.AST.Comments {
			if off(cg.Pos()) >= sp.from && off(cg.End()) <= sp.to {
				b.WriteString(cg.Text())
				b.WriteByte('\n')
			}
		}
		sp.comments = b.String()
		decls = append(decls, sp)
	}
	cites := map[string]bool{}
	for _, p := range citedPaths(g.Src) {
		cites[p] = true
	}
	var funcs []goTestFunc
	for _, d := range g.AST.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
			for _, sp := range decls {
				if sp.name == fd.Name.Name {
					funcs = append(funcs, goTestFunc{File: g.Rel, Name: fd.Name.Name, Deviated: deviatedRe.MatchString(sp.comments), Cites: cites})
					break
				}
			}
		}
	}
	groupOf := func(o int) (int, int) {
		for _, cg := range g.AST.Comments {
			if o >= off(cg.Pos()) && o < off(cg.End()) {
				return off(cg.Pos()), off(cg.End())
			}
		}
		return o, o
	}

	var out []citation
	for _, m := range citeRe.FindAllStringSubmatchIndex(g.Src, -1) {
		c := citation{
			File: g.Rel,
			Line: strings.Count(g.Src[:m[0]], "\n") + 1,
			Path: strings.TrimPrefix(g.Src[m[2]:m[3]], "./"),
		}
		if m[4] >= 0 {
			for _, r := range numRangeRe.FindAllStringSubmatch(g.Src[m[4]:m[5]], -1) {
				lo, _ := strconv.Atoi(r[1])
				hi := lo
				if r[2] != "" {
					hi, _ = strconv.Atoi(r[2])
				}
				c.Ranges = append(c.Ranges, [2]int{lo, hi})
			}
		}
		inDecl := false
		for _, d := range decls {
			if m[0] >= d.from && m[0] < d.to {
				inDecl = true
				c.Context = d.name
				c.Deviated = deviatedRe.MatchString(d.comments)
				break
			}
		}
		if !inDecl {
			gs, ge := groupOf(m[0])
			s := sentenceAround(g.Src, gs, ge, m[0], m[1])
			c.Sentence = s
			c.Deviated = deviatedRe.MatchString(s)
			c.NotApplicable = naRe.MatchString(s)
			c.Deferred = deferredRe.MatchString(s)
		}
		out = append(out, c)
	}
	return out, funcs
}

// sentenceAround returns the comment sentence holding [cs, ce) within
// the comment group [gs, ge), with the `//` markers removed.
func sentenceAround(src string, gs, ge, cs, ce int) string {
	start := gs
	for k := cs - 1; k > gs; k-- {
		if src[k] == '.' || src[k] == ';' {
			if k+1 < len(src) && (src[k+1] == ' ' || src[k+1] == '\n' || src[k+1] == '\r') {
				start = k + 1
				break
			}
		}
	}
	end := ge
	for k := ce; k < ge; k++ {
		if src[k] == '.' || src[k] == ';' {
			if k+1 >= ge || src[k+1] == ' ' || src[k+1] == '\n' || src[k+1] == '\r' {
				end = k + 1
				break
			}
		}
	}
	text := src[start:end]
	var parts []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "//"))
		if ln != "" {
			parts = append(parts, ln)
		}
	}
	return strings.Join(parts, " ")
}
