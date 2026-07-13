// Command markdown-ast parses Markdown with Goldmark's CommonMark/GFM parser
// and prints the links and headings used by the repository documentation gates.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type markdownLink struct {
	Line        int    `json:"line"`
	Destination string `json:"destination"`
}

type markdownHeading struct {
	Line  int    `json:"line"`
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

type markdownDocument struct {
	Path     string            `json:"path"`
	Links    []markdownLink    `json:"links"`
	Headings []markdownHeading `json:"headings"`
}

type markdownOutput struct {
	Documents []markdownDocument `json:"documents"`
}

type githubIDs struct {
	used map[string]struct{}
}

func newGitHubIDs() *githubIDs {
	return &githubIDs{used: make(map[string]struct{})}
}

func (ids *githubIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	var slug strings.Builder
	for _, r := range strings.ToLower(string(value)) {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '_':
			slug.WriteRune(r)
		case unicode.IsSpace(r), r == '-':
			slug.WriteByte('-')
		}
	}
	base := slug.String()
	if base == "" {
		if kind == ast.KindHeading {
			base = "heading"
		} else {
			base = "id"
		}
	}
	return []byte(ids.reserve(base))
}

func (ids *githubIDs) Put(value []byte) {
	ids.used[string(value)] = struct{}{}
}

func (ids *githubIDs) reserve(base string) string {
	if _, exists := ids.used[base]; !exists {
		ids.used[base] = struct{}{}
		return base
	}
	for suffix := 1; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if _, exists := ids.used[candidate]; !exists {
			ids.used[candidate] = struct{}{}
			return candidate
		}
	}
}

func lineNumber(source []byte, offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(source) {
		offset = len(source)
	}
	return bytes.Count(source[:offset], []byte{'\n'}) + 1
}

func nodeOffset(node ast.Node) int {
	minimum := -1
	_ = ast.Walk(node, func(current ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if value, ok := current.(*ast.Text); ok && value.Segment.Start >= 0 {
			if minimum == -1 || value.Segment.Start < minimum {
				minimum = value.Segment.Start
			}
		}
		return ast.WalkContinue, nil
	})
	if minimum >= 0 {
		return minimum
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Type() == ast.TypeBlock && current.Lines().Len() > 0 {
			return current.Lines().At(0).Start
		}
	}
	return 0
}

func headingID(heading *ast.Heading) string {
	value, ok := heading.AttributeString("id")
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func parseMarkdown(path string, source []byte) markdownDocument {
	markdown := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithAttribute(),
		),
	)
	context := parser.NewContext(parser.WithIDs(newGitHubIDs()))
	root := markdown.Parser().Parse(text.NewReader(source), parser.WithContext(context))
	document := markdownDocument{
		Path:     path,
		Links:    make([]markdownLink, 0),
		Headings: make([]markdownHeading, 0),
	}
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch value := node.(type) {
		case *ast.Link:
			document.Links = append(document.Links, markdownLink{
				Line:        lineNumber(source, nodeOffset(value)),
				Destination: string(value.Destination),
			})
		case *ast.Image:
			document.Links = append(document.Links, markdownLink{
				Line:        lineNumber(source, nodeOffset(value)),
				Destination: string(value.Destination),
			})
		case *ast.Heading:
			offset := 0
			if value.Lines().Len() > 0 {
				offset = value.Lines().At(0).Start
			}
			document.Headings = append(document.Headings, markdownHeading{
				Line:  lineNumber(source, offset),
				Level: value.Level,
				Text:  strings.TrimSpace(string(value.Text(source))),
				ID:    headingID(value),
			})
		}
		return ast.WalkContinue, nil
	})
	return document
}

func run(paths []string) error {
	sort.Strings(paths)
	output := markdownOutput{Documents: make([]markdownDocument, 0, len(paths))}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		output.Documents = append(output.Documents, parseMarkdown(path, source))
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/markdown-ast.go MARKDOWN...")
		os.Exit(2)
	}
	if err := run(append([]string(nil), os.Args[1:]...)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
