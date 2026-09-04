package stats

import "testing"

func TestCountWords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"simple sentence", "The quick brown fox jumps.", 5},
		{"hyphenated compound counts once", "A well-known fact.", 3},
		{"em dash separates two words", "wait—really", 2},
		{"punctuation only tokens excluded", "-- ... !!", 0},
		{"numbers count as words", "There are 3 apples and 4 oranges.", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountWords(tc.in); got != tc.want {
				t.Errorf("CountWords(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestCountSentences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"single sentence", "This is one sentence.", 1},
		{"two sentences", "First one. Second one.", 2},
		{"question and exclamation", "Really? Yes!", 2},
		{"no terminal punctuation still counts one", "Trailing thought with no period", 1},
		{"abbreviation does not split", "I met Dr. Smith yesterday.", 1},
		{"multiple abbreviations do not split", "Meet Mr. Jones and Mrs. Lee soon.", 1},
		{"single letter initials do not split", "C. S. Lewis wrote this.", 1},
		{"decimal does not split", "Pi is about 3.14 and that's neat.", 1},
		{"ellipsis is one break not three", "Wait... what?", 2},
		{"period inside a word does not split", "Visit example.com for more.", 1},
		{"e.g. does not split", "Bring supplies, e.g. water and food.", 1},
		{"u.s. does not split", "She moved to the U.S. last year.", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountSentences(tc.in); got != tc.want {
				t.Errorf("CountSentences(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestSyllables(t *testing.T) {
	cases := []struct {
		word string
		want int
	}{
		{"cat", 1},
		{"table", 2},
		{"make", 1},
		{"battle", 2},
		{"boxes", 2},
		{"wanted", 2},
		{"jumped", 1},
		{"the", 1},
		{"a", 1},
		{"banana", 3},
		{"beautiful", 3},
	}
	for _, tc := range cases {
		t.Run(tc.word, func(t *testing.T) {
			if got := Syllables(tc.word); got != tc.want {
				t.Errorf("Syllables(%q) = %d, want %d", tc.word, got, tc.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		seconds int
		want    string
	}{
		{0, "0s"},
		{1, "1s"},
		{48, "48s"},
		{59, "59s"},
		{60, "1m0s"},
		{116, "1m56s"},
		{532, "8m52s"},
	}
	for _, tc := range cases {
		if got := FormatDuration(tc.seconds); got != tc.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestComputeEmptyProse(t *testing.T) {
	got := Compute("", 200)
	if got.Words != 0 || got.Sentences != 0 || got.Syllables != 0 {
		t.Fatalf("expected all-zero counts for empty prose, got %+v", got)
	}
	if got.ReadingSeconds != 0 || got.ReadingTime != "0s" {
		t.Fatalf("expected zero reading time for empty prose, got %+v", got)
	}
	if got.ReadingEase != 0 || got.GradeLevel != 0 {
		t.Fatalf("expected zero scores for empty prose (not NaN), got %+v", got)
	}
}

func TestComputeNonEmptyNeverZeroSeconds(t *testing.T) {
	got := Compute("one", 200000)
	if got.ReadingSeconds < 1 {
		t.Fatalf("expected reading seconds floored at 1 for non-empty post, got %d", got.ReadingSeconds)
	}
}

func TestFleschScoresClamped(t *testing.T) {
	// A pathological, extremely short/simple input shouldn't push the
	// ease score above 100 or the grade level below 0.
	ease := FleschReadingEase(1, 1, 1)
	if ease < 0 || ease > 100 {
		t.Fatalf("FleschReadingEase out of range: %v", ease)
	}
	grade := FleschKincaidGrade(1, 100, 1)
	if grade < 0 {
		t.Fatalf("FleschKincaidGrade should be floored at 0, got %v", grade)
	}
}

func TestEaseBand(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{95, "Very Easy"},
		{85, "Easy"},
		{75, "Fairly Easy"},
		{65, "Standard"},
		{55, "Fairly Difficult"},
		{35, "Difficult"},
		{10, "Very Confusing"},
	}
	for _, tc := range cases {
		if got := EaseBand(tc.score); got != tc.want {
			t.Errorf("EaseBand(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestFromCountsMatchesSummedTotals(t *testing.T) {
	a := FromCounts(100, 5, 150, 200)
	b := FromCounts(200, 10, 300, 200)
	total := FromCounts(a.Words+b.Words, a.Sentences+b.Sentences, a.Syllables+b.Syllables, 200)
	if total.Words != 300 || total.Sentences != 15 || total.Syllables != 450 {
		t.Fatalf("unexpected summed totals: %+v", total)
	}
}

// Regression: the "-es" rule used to strip a syllable from stems ending in a
// soft c/g or the "i" of a y-to-ies plural, biasing every score on ordinary
// prose. The second half guards the words that should still lose a syllable.
func TestSyllablesInflectedPlurals(t *testing.T) {
	tests := map[string]int{
		"cities":    2,
		"parties":   2,
		"companies": 3,
		"faces":     2,
		"changes":   2,
		"offices":   3,
		"villages":  3,
		"movies":    2,
		"houses":    2,
		"matches":   2,
		"makes":     1,
		"times":     1,
		"places":    2,
		"ties":      1,
		"goes":      1,
	}
	for word, want := range tests {
		if got := Syllables(word); got != want {
			t.Errorf("Syllables(%q) = %d, want %d", word, got, want)
		}
	}
}

// Regression: terminal punctuation inside a quotation, and abbreviations whose
// periods are glued together, both used to inflate the sentence count -- which
// is a divisor in both readability formulas.
func TestCountSentencesQuotesAndGluedAbbreviations(t *testing.T) {
	tests := map[string]int{
		`He said "Stop!" and left. Then he sat down.`:            2,
		`She asked, "Are you coming?" and waited.`:               1,
		`"Are you coming?" She waited.`:                          2,
		"J.R.R. Tolkien wrote this. It is long.":                 2,
		"She met J.K. Rowling once.":                             1,
		"She moved to the U.S.A. last year. Then she came back.": 2,
		"C. S. Lewis wrote it. Then he stopped.":                 2,
		"Pi is 3.14 exactly. Yes.":                               2,
	}
	for text, want := range tests {
		if got := CountSentences(text); got != want {
			t.Errorf("CountSentences(%q) = %d, want %d", text, got, want)
		}
	}
}
