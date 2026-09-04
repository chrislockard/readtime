// Package parse reduces raw Hugo markdown files down to plain prose so that
// internal/stats can count words and sentences without markdown syntax,
// shortcodes, or HTML noise skewing the results.
package parse

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Result holds the plain-prose text extracted from a post along with the
// front matter fields main.go needs to decide whether to include the post
// and what to call it.
type Result struct {
	Title string
	Draft bool
	Prose string
}

// codePlaceholder stands in for a stripped inline code span. A code span
// almost always plays the role of a noun in the surrounding sentence, so a
// single word keeps word/sentence counting sane instead of deleting the
// token outright.
const codePlaceholder = "code"

// Parse strips markdown/Hugo constructs from raw and returns the remaining
// prose plus the front matter fields of interest.
func Parse(raw []byte) Result {
	src := normalizeNewlines(string(raw))

	fm, body, kind := splitFrontMatter(src)
	title, draft := parseFrontMatter(fm, kind)

	body = stripCode(body)
	body = stripShortcodes(body)
	body = stripLinks(body)
	body = stripHTML(body)
	body = stripMarkdownSyntax(body)

	return Result{
		Title: title,
		Draft: draft,
		Prose: collapseWhitespace(body),
	}
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// ---------------------------------------------------------------------------
// Front matter
// ---------------------------------------------------------------------------

// splitFrontMatter removes a leading YAML (---), TOML (+++), or JSON ({})
// front matter block, which must start on line 1. kind is "yaml", "toml",
// "json", or "" if no front matter block was found.
func splitFrontMatter(s string) (fm, body, kind string) {
	switch {
	case strings.HasPrefix(s, "---\n") || s == "---":
		return splitDelimited(s, "---", "yaml")
	case strings.HasPrefix(s, "+++\n") || s == "+++":
		return splitDelimited(s, "+++", "toml")
	case looksLikeJSONFrontMatter(s):
		return splitJSON(s)
	default:
		return "", s, ""
	}
}

func splitDelimited(s, delim, kind string) (fm, body, k string) {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == delim {
			fm = strings.Join(lines[1:i], "\n")
			body = strings.Join(lines[i+1:], "\n")
			return fm, body, kind
		}
	}
	// No closing delimiter found: treat the whole file as front matter-less
	// prose rather than silently eating the entire post.
	return "", s, ""
}

// splitJSON finds the end of a JSON front matter block by tracking brace
// depth, respecting quoted strings so braces inside string values don't
// confuse the scan.
// looksLikeJSONFrontMatter distinguishes a real JSON front matter block from
// a post that merely opens with a brace -- most importantly a Hugo shortcode
// call such as "{{< picture >}}", which a bare "{" prefix test would swallow
// as front matter and then leave the rest of the document mispaired.
func looksLikeJSONFrontMatter(s string) bool {
	if !strings.HasPrefix(s, "{") || strings.HasPrefix(s, "{{") {
		return false
	}
	for _, r := range s[1:] {
		if unicode.IsSpace(r) {
			continue
		}
		// A JSON object body starts with a quoted key, or closes immediately.
		return r == '"' || r == '}'
	}
	return false
}

func splitJSON(s string) (fm, body, kind string) {
	depth := 0
	inString := false
	escaped := false
	for i, r := range s {
		if inString {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1], s[i+1:], "json"
			}
		}
	}
	// Unterminated JSON block: no safe split point, so leave it untouched.
	return "", s, ""
}

var (
	yamlTitleRe = regexp.MustCompile(`(?im)^\s*title\s*:\s*(.+?)\s*$`)
	tomlTitleRe = regexp.MustCompile(`(?im)^\s*title\s*=\s*(.+?)\s*$`)
	yamlDraftRe = regexp.MustCompile(`(?im)^\s*draft\s*:\s*"?(true|false)"?\s*$`)
	tomlDraftRe = regexp.MustCompile(`(?im)^\s*draft\s*=\s*"?(true|false)"?\s*$`)
)

func parseFrontMatter(fm, kind string) (title string, draft bool) {
	switch kind {
	case "json":
		var doc struct {
			Title string `json:"title"`
			Draft bool   `json:"draft"`
		}
		// Front matter that fails to parse just yields the zero value; a
		// malformed post shouldn't crash the whole run.
		_ = json.Unmarshal([]byte(fm), &doc)
		return doc.Title, doc.Draft
	case "toml":
		title = unquote(firstMatch(tomlTitleRe, fm))
		draft = strings.EqualFold(firstMatch(tomlDraftRe, fm), "true")
		return title, draft
	case "yaml":
		title = unquote(firstMatch(yamlTitleRe, fm))
		draft = strings.EqualFold(firstMatch(yamlDraftRe, fm), "true")
		return title, draft
	default:
		return "", false
	}
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Code: fenced blocks, indented blocks, inline spans
// ---------------------------------------------------------------------------

var fenceRe = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")

func stripCode(s string) string {
	s = stripFencedCode(s)
	s = stripIndentedCode(s)
	s = stripCodeSpans(s)
	return s
}

func stripFencedCode(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	inFence := false
	var fenceChar byte
	var fenceLen int
	for _, line := range lines {
		if !inFence {
			if m := fenceRe.FindStringSubmatch(line); m != nil {
				inFence = true
				fenceChar = m[2][0]
				fenceLen = len(m[2])
				continue
			}
			out = append(out, line)
			continue
		}
		// Inside a fence: look for a closing line of the same character,
		// at least as long as the opener, honoring the CommonMark "longer
		// fences" rule.
		trimmed := strings.TrimLeft(line, " ")
		if isClosingFence(trimmed, fenceChar, fenceLen) {
			inFence = false
			continue
		}
		// Drop the code line itself.
	}
	return strings.Join(out, "\n")
}

func isClosingFence(trimmed string, ch byte, minLen int) bool {
	if len(trimmed) < minLen {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != ch {
			return false
		}
	}
	return true
}

// stripIndentedCode drops 4-space/tab indented code blocks. Per CommonMark,
// an indented block only starts where the preceding line is blank (or is
// the start of the document/another indented line) so ordinary indented
// text inside a paragraph or list continuation isn't mistaken for code.
func stripIndentedCode(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	prevBlank := true
	inBlock := false
	for _, line := range lines {
		indented := isIndentedCodeLine(line)
		blank := strings.TrimSpace(line) == ""
		switch {
		case inBlock && (indented || blank):
			// Still inside the block; drop the line. A blank line stays
			// part of the block only if code resumes afterward, but since
			// we're dropping content wholesale we don't need to look ahead.
		case indented && prevBlank:
			inBlock = true
		default:
			inBlock = false
			out = append(out, line)
		}
		if !blank {
			prevBlank = false
		} else {
			prevBlank = true
		}
	}
	return strings.Join(out, "\n")
}

func isIndentedCodeLine(line string) bool {
	if strings.HasPrefix(line, "\t") {
		return true
	}
	spaces := 0
	for _, r := range line {
		if r == ' ' {
			spaces++
			continue
		}
		break
	}
	return spaces >= 4 && spaces < len(line)
}

// stripCodeSpans replaces `code` / “code“ inline spans with a single
// placeholder word. Go's RE2 engine can't backreference the opening
// backtick run length, so this walks the string by hand.
func stripCodeSpans(s string) string {
	var b strings.Builder
	i := 0
	n := len(s)
	for i < n {
		if s[i] != '`' {
			b.WriteByte(s[i])
			i++
			continue
		}
		start := i
		for i < n && s[i] == '`' {
			i++
		}
		openLen := i - start
		// Find a closing run of exactly the same length.
		j := i
		found := -1
		for j < n {
			if s[j] == '`' {
				k := j
				for k < n && s[k] == '`' {
					k++
				}
				if k-j == openLen {
					found = j
					i = k
					break
				}
				j = k
				continue
			}
			j++
		}
		if found == -1 {
			// No closing run: not actually a code span, emit the backticks
			// literally and keep scanning.
			b.WriteString(s[start:i])
			continue
		}
		b.WriteString(codePlaceholder)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Hugo shortcodes
// ---------------------------------------------------------------------------

// shortcodes that wrap raw code rather than prose: dropping the tags alone
// would leave source code in the word count.
var codeShortcodes = map[string]bool{
	"highlight": true,
	"gist":      true,
	"codeblock": true,
}

var (
	angleTagRe   = regexp.MustCompile(`(?s)\{\{<\s*(/?)\s*([A-Za-z][\w-]*)[^{}]*?>\}\}`)
	percentTagRe = regexp.MustCompile(`(?s)\{\{%\s*(/?)\s*([A-Za-z][\w-]*)[^{}]*?%\}\}`)
)

type shortcodeTag struct {
	start, end int
	name       string
	closing    bool
}

type deletion struct {
	start, end int
}

// stripShortcodes removes Hugo shortcode tags. Paired shortcodes (an
// opening tag matched by a later closing tag of the same name) keep their
// inner prose unless the shortcode is known to wrap code; unmatched
// (void/self-closing) tags are simply removed.
func stripShortcodes(s string) string {
	var tags []shortcodeTag
	for _, re := range []*regexp.Regexp{angleTagRe, percentTagRe} {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			tags = append(tags, shortcodeTag{
				start:   m[0],
				end:     m[1],
				closing: m[3] > m[2],
				name:    s[m[4]:m[5]],
			})
		}
	}
	if len(tags) == 0 {
		return s
	}
	sortTagsByStart(tags)

	var deletions []deletion
	consumed := make([]bool, len(tags))

	// Pair each opening tag with the next unclaimed closing tag of the same
	// name, in document order.
	pending := map[string][]int{} // name -> indices of unmatched opens
	for i, t := range tags {
		if !t.closing {
			pending[t.name] = append(pending[t.name], i)
			continue
		}
		stack := pending[t.name]
		if len(stack) == 0 {
			// Stray closing tag with no opener: drop the tag itself.
			deletions = append(deletions, deletion{t.start, t.end})
			consumed[i] = true
			continue
		}
		openIdx := stack[len(stack)-1]
		pending[t.name] = stack[:len(stack)-1]
		consumed[openIdx] = true
		consumed[i] = true
		openTag := tags[openIdx]
		if codeShortcodes[t.name] {
			deletions = append(deletions, deletion{openTag.start, t.end})
		} else {
			deletions = append(deletions, deletion{openTag.start, openTag.end})
			deletions = append(deletions, deletion{t.start, t.end})
		}
	}
	// Anything left unmatched is a void/self-closing shortcode call: drop
	// just the tag.
	for i, t := range tags {
		if consumed[i] {
			continue
		}
		deletions = append(deletions, deletion{t.start, t.end})
	}

	sortDeletionsByStart(deletions)
	var b strings.Builder
	pos := 0
	for _, d := range deletions {
		if d.start < pos {
			continue // overlapping deletion, already covered
		}
		b.WriteString(s[pos:d.start])
		b.WriteByte(' ')
		pos = d.end
	}
	b.WriteString(s[pos:])
	return b.String()
}

func sortTagsByStart(tags []shortcodeTag) {
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0 && tags[j-1].start > tags[j].start; j-- {
			tags[j-1], tags[j] = tags[j], tags[j-1]
		}
	}
}

func sortDeletionsByStart(d []deletion) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j-1].start > d[j].start; j-- {
			d[j-1], d[j] = d[j], d[j-1]
		}
	}
}

// ---------------------------------------------------------------------------
// Links, images, autolinks, bare URLs
// ---------------------------------------------------------------------------

var (
	imageRe    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	linkRe     = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	footnoteRe = regexp.MustCompile(`\[\^[^\]]+\]`)
	refDefRe   = regexp.MustCompile(`(?m)^\s*\[[^\]^][^\]]*\]:\s*\S+.*$`)
	autolinkRe = regexp.MustCompile(`<(https?|ftp)://[^>\s]+>`)
	bareURLRe  = regexp.MustCompile(`\bhttps?://\S+`)
)

func stripLinks(s string) string {
	s = refDefRe.ReplaceAllString(s, "")
	s = imageRe.ReplaceAllString(s, "")
	s = linkRe.ReplaceAllString(s, "$1")
	s = autolinkRe.ReplaceAllString(s, "")
	s = bareURLRe.ReplaceAllString(s, "")
	return s
}

// ---------------------------------------------------------------------------
// HTML
// ---------------------------------------------------------------------------

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlTagRe     = regexp.MustCompile(`(?s)<[^>]+>`)
)

func stripHTML(s string) string {
	s = scriptStyleRe.ReplaceAllString(s, " ")
	s = htmlTagRe.ReplaceAllString(s, " ")
	return s
}

// ---------------------------------------------------------------------------
// Remaining markdown syntax: headings, lists, blockquotes, tables, rules,
// emphasis, footnote markers
// ---------------------------------------------------------------------------

var (
	headingRe     = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	headingTailRe = regexp.MustCompile(`\s+#+\s*$`)
	bulletRe      = regexp.MustCompile(`^\s*[-*+]\s+`)
	orderedRe     = regexp.MustCompile(`^\s*\d+[.)]\s+`)
	blockquoteRe  = regexp.MustCompile(`^\s*>+\s?`)
	tableSepRe    = regexp.MustCompile(`^\s*\|?[\s:|-]*-[\s:|-]*\|?\s*$`)
	footnoteDefRe = regexp.MustCompile(`^\s*\[\^[^\]]+\]:\s*`)
	emphasisRe    = regexp.MustCompile(`[*_~]+`)
)

// isThematicBreak reports whether line, once whitespace is ignored, is
// three or more of the same "-", "*", or "_" character (CommonMark's
// horizontal rule / thematic break). RE2 can't backreference a captured
// character class, so this is checked by hand rather than with a regexp.
func isThematicBreak(line string) bool {
	var ch byte
	count := 0
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if c != '-' && c != '*' && c != '_' {
			return false
		}
		if ch == 0 {
			ch = c
		} else if c != ch {
			return false
		}
		count++
	}
	return count >= 3
}

func stripMarkdownSyntax(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		if isThematicBreak(line) {
			continue
		}
		if tableSepRe.MatchString(line) && strings.Contains(line, "-") {
			continue
		}
		line = footnoteDefRe.ReplaceAllString(line, "")
		line = headingRe.ReplaceAllString(line, "")
		line = headingTailRe.ReplaceAllString(line, "")
		line = blockquoteRe.ReplaceAllString(line, "")
		line = bulletRe.ReplaceAllString(line, "")
		line = orderedRe.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "|", " ")
		out = append(out, line)
	}
	s = strings.Join(out, "\n")
	s = footnoteRe.ReplaceAllString(s, "")
	s = emphasisRe.ReplaceAllString(s, "")
	return s
}

// ---------------------------------------------------------------------------
// Whitespace
// ---------------------------------------------------------------------------

func collapseWhitespace(s string) string {
	var b strings.Builder
	lastSpace := true // trim leading space
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
			}
			lastSpace = true
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}
