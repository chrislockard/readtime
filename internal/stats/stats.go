// Package stats counts words and sentences in plain prose and derives
// reading time and readability scores from those counts.
package stats

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// Stats holds the counts and derived scores for a single post, or for the
// aggregated totals across a run.
type Stats struct {
	Words           int
	Sentences       int
	Syllables       int
	ReadingSeconds  int
	ReadingTime     string
	ReadingEase     float64
	ReadingEaseBand string
	GradeLevel      float64
}

// Compute analyzes prose (already stripped of markdown/HTML by
// internal/parse) at the given words-per-minute reading speed.
func Compute(prose string, wpm int) Stats {
	tokens := Words(prose)
	words := len(tokens)
	sentences := CountSentences(prose)
	if words > 0 && sentences == 0 {
		sentences = 1
	}
	syllables := 0
	for _, w := range tokens {
		syllables += Syllables(w)
	}
	return FromCounts(words, sentences, syllables, wpm)
}

// FromCounts builds a Stats from raw counts, recomputing reading time and
// readability scores. Used both by Compute and by main.go to build the
// aggregate totals row (which sums counts across files rather than
// averaging pre-computed scores).
func FromCounts(words, sentences, syllables, wpm int) Stats {
	seconds := readingSeconds(words, wpm)
	ease := round1(FleschReadingEase(words, sentences, syllables))
	grade := round1(FleschKincaidGrade(words, sentences, syllables))
	return Stats{
		Words:           words,
		Sentences:       sentences,
		Syllables:       syllables,
		ReadingSeconds:  seconds,
		ReadingTime:     FormatDuration(seconds),
		ReadingEase:     ease,
		ReadingEaseBand: EaseBand(ease),
		GradeLevel:      grade,
	}
}

// ---------------------------------------------------------------------------
// Words
// ---------------------------------------------------------------------------

// Words splits prose into word tokens: whitespace-separated substrings
// containing at least one letter or digit. An em dash between two words
// (with no surrounding whitespace) separates them into two words, unlike a
// hyphen, which keeps a compound as a single word.
func Words(prose string) []string {
	prose = strings.ReplaceAll(prose, "—", " ")
	fields := strings.Fields(prose)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if hasAlnum(f) {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// CountWords returns the number of words in prose.
func CountWords(prose string) int {
	return len(Words(prose))
}

// ---------------------------------------------------------------------------
// Sentences
// ---------------------------------------------------------------------------

// abbreviations that end in a period but never end a sentence by
// themselves, keyed by their lowercased text including the trailing period.
var abbreviations = map[string]bool{
	"mr.": true, "mrs.": true, "dr.": true, "prof.": true, "st.": true,
	"jr.": true, "sr.": true, "vs.": true, "etc.": true,
	"e.g.": true, "i.e.": true, "a.m.": true, "p.m.": true, "u.s.": true,
}

// CountSentences counts sentences by looking for runs of terminal
// punctuation (./!/?), skipping breaks that are actually abbreviations,
// single-letter initials, decimals, or a period inside a word. Ellipses
// count as a single break rather than one per dot. Trailing text with no
// terminal punctuation still counts as one sentence.
func CountSentences(prose string) int {
	runes := []rune(prose)
	n := len(runes)
	count := 0
	lastBreakEnd := 0

	isTerminal := func(r rune) bool { return r == '.' || r == '!' || r == '?' }

	i := 0
	for i < n {
		if !isTerminal(runes[i]) {
			i++
			continue
		}
		start := i
		for i < n && isTerminal(runes[i]) {
			i++
		}
		end := i

		breakHere := true

		// A run attached directly to the next non-space, non-closing
		// character (no whitespace/EOF between them) is not a sentence
		// break: this covers decimals ("3.14"), abbreviations glued to the
		// next letter ("e.g."), and a period inside a word.
		k := end
		for k < n && isClosingPunct(runes[k]) {
			k++
		}
		if k < n && !unicode.IsSpace(runes[k]) {
			breakHere = false
		}

		// Terminal punctuation inside a quotation that the sentence
		// continues past: `He said "Stop!" and left.` is one sentence, not
		// two. A closing quote followed by a lowercase word means the
		// quoted clause is embedded rather than terminal.
		if breakHere && k > end {
			j := k
			for j < n && unicode.IsSpace(runes[j]) {
				j++
			}
			if j < n && unicode.IsLower(runes[j]) {
				breakHere = false
			}
		}

		if breakHere && end-start == 1 && runes[start] == '.' {
			tokenStart := start
			for tokenStart > 0 && !unicode.IsSpace(runes[tokenStart-1]) {
				tokenStart--
			}
			token := strings.ToLower(string(runes[tokenStart:end]))
			if abbreviations[token] {
				breakHere = false
			} else if letters := token[:len(token)-1]; isSingleLetter(letters) {
				breakHere = false
			} else if isGluedInitials(token) {
				breakHere = false
			}
		}

		if breakHere {
			count++
			lastBreakEnd = end
		}
	}

	if hasNonSpace(runes[lastBreakEnd:]) {
		count++
	}
	return count
}

func isClosingPunct(r rune) bool {
	switch r {
	case '"', '\'', ')', ']', '”', '’':
		return true
	}
	return false
}

// isGluedInitials reports whether a token is a run of single letters each
// followed by a period, with no spaces between them: "j.r.r.", "u.s.a.".
// The spaced form ("C. S. Lewis") is handled by isSingleLetter instead.
func isGluedInitials(token string) bool {
	rs := []rune(token)
	if len(rs) < 4 || rs[len(rs)-1] != '.' {
		return false
	}
	for i := 0; i < len(rs); i += 2 {
		if !unicode.IsLetter(rs[i]) {
			return false
		}
		if i+1 >= len(rs) || rs[i+1] != '.' {
			return false
		}
	}
	return true
}

func isSingleLetter(s string) bool {
	rs := []rune(s)
	return len(rs) == 1 && unicode.IsLetter(rs[0])
}

func hasNonSpace(runes []rune) bool {
	for _, r := range runes {
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Syllables
// ---------------------------------------------------------------------------

// Syllables estimates the syllable count of a single word using a
// vowel-group heuristic: lowercase, count consecutive-vowel groups in
// "aeiouy", drop a silent trailing "e" (but not "-le" after a consonant, as
// in "table"), and adjust for "-es"/"-ed" suffixes that don't usually add a
// syllable of their own. Always returns at least 1.
func Syllables(word string) int {
	letters := extractLetters(strings.ToLower(word))
	if letters == "" {
		return 1
	}

	groups := countVowelGroups(letters)

	switch {
	case strings.HasSuffix(letters, "le") && len(letters) >= 3 && !isVowelByte(letters[len(letters)-3]):
		// "table", "cradle": the "-le" carries its own syllable, so the
		// silent-e rule below doesn't apply.
	case strings.HasSuffix(letters, "e"):
		groups--
	}

	if strings.HasSuffix(letters, "es") {
		stem := letters[:len(letters)-2]
		if stem != "" && !endsInSibilant(stem) {
			groups--
		}
	} else if strings.HasSuffix(letters, "ed") {
		stem := letters[:len(letters)-2]
		if stem != "" {
			last := stem[len(stem)-1]
			if last != 't' && last != 'd' {
				groups--
			}
		}
	}

	if groups < 1 {
		groups = 1
	}
	return groups
}

// endsInSibilant reports whether a stem takes an audible "-es" syllable, so
// that "faces", "changes" and "cities" keep the syllable that "makes" and
// "times" correctly lose. Beyond the literal sibilants this covers soft "c"
// and "g" (face, change) and the "i" left behind by a y-to-ies plural (city).
func endsInSibilant(stem string) bool {
	if strings.HasSuffix(stem, "ch") || strings.HasSuffix(stem, "sh") {
		return true
	}
	switch stem[len(stem)-1] {
	case 's', 'z', 'x', 'j', 'c', 'g', 'i':
		return true
	}
	return false
}

func extractLetters(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isVowelByte(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u', 'y':
		return true
	}
	return false
}

func countVowelGroups(s string) int {
	groups := 0
	inGroup := false
	for i := 0; i < len(s); i++ {
		if isVowelByte(s[i]) {
			if !inGroup {
				groups++
				inGroup = true
			}
		} else {
			inGroup = false
		}
	}
	return groups
}

// ---------------------------------------------------------------------------
// Reading time
// ---------------------------------------------------------------------------

func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

func readingSeconds(words, wpm int) int {
	if words == 0 {
		return 0
	}
	if wpm <= 0 {
		wpm = 1
	}
	seconds := int(math.Round(float64(words) / float64(wpm) * 60))
	if seconds < 1 {
		seconds = 1 // never report 0s for a non-empty post
	}
	return seconds
}

// FormatDuration renders a second count as "1m56s" or, for sub-minute
// durations, just "48s".
func FormatDuration(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	m := seconds / 60
	s := seconds % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

// ---------------------------------------------------------------------------
// Readability
// ---------------------------------------------------------------------------

// FleschReadingEase computes the Flesch Reading Ease score, clamped to
// [0, 100].
func FleschReadingEase(words, sentences, syllables int) float64 {
	if words == 0 {
		return 0
	}
	if sentences < 1 {
		sentences = 1
	}
	score := 206.835 - 1.015*(float64(words)/float64(sentences)) - 84.6*(float64(syllables)/float64(words))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// FleschKincaidGrade computes the Flesch-Kincaid Grade Level, floored at 0.
func FleschKincaidGrade(words, sentences, syllables int) float64 {
	if words == 0 {
		return 0
	}
	if sentences < 1 {
		sentences = 1
	}
	grade := 0.39*(float64(words)/float64(sentences)) + 11.8*(float64(syllables)/float64(words)) - 15.59
	if grade < 0 {
		grade = 0
	}
	return grade
}

// EaseBand labels a Flesch Reading Ease score with its standard band name.
func EaseBand(score float64) string {
	switch {
	case score >= 90:
		return "Very Easy"
	case score >= 80:
		return "Easy"
	case score >= 70:
		return "Fairly Easy"
	case score >= 60:
		return "Standard"
	case score >= 50:
		return "Fairly Difficult"
	case score >= 30:
		return "Difficult"
	default:
		return "Very Confusing"
	}
}
