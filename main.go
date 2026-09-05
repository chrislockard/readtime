// Command readtime analyzes text files -- Hugo blog posts (markdown) and
// plain prose alike -- and reports word count, sentence count, estimated
// reading time, and readability scores.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/chrislockard/readtime/internal/parse"
	"github.com/chrislockard/readtime/internal/stats"
)

const version = "0.1.0"

// skipDirs are directory names readtime never descends into: Hugo build
// output and dependency directories would otherwise pollute the results.
var skipDirs = map[string]bool{
	"public":       true,
	"resources":    true,
	"node_modules": true,
}

// entry is a single analyzed file (or the lone stdin post) ready for
// output.
type entry struct {
	Display string
	Title   string
	Stats   stats.Stats
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("readtime", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: readtime [flags] <path>...")
		fs.PrintDefaults()
	}

	jsonOut := fs.Bool("json", false, "emit JSON instead of the table")
	wpm := fs.Int("wpm", 200, "reading speed in words per minute (Hugo itself uses 213)")
	sortKey := fs.String("sort", "name", "sort by name, words, time, or grade")
	includeDrafts := fs.Bool("drafts", false, "include posts with draft: true in front matter")
	showVersion := fs.Bool("version", false, "print version and exit")
	sniffBytes := fs.Int("sniff-bytes", 512, "bytes read from a file's start to decide if it's text")

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *showVersion {
		fmt.Fprintln(stdout, "readtime "+version)
		return 0
	}
	if *sniffBytes <= 0 {
		fmt.Fprintf(stderr, "readtime: invalid --sniff-bytes value %d (want a positive number)\n", *sniffBytes)
		return 1
	}
	switch *sortKey {
	case "name", "words", "time", "grade":
	default:
		fmt.Fprintf(stderr, "readtime: invalid --sort value %q (want name, words, time, or grade)\n", *sortKey)
		return 1
	}

	paths := fs.Args()

	var entries []entry
	if len(paths) == 0 {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(stderr, "readtime: stdin: %v\n", err)
			return 1
		}
		e, ok := analyze("(stdin)", raw, *includeDrafts, *wpm)
		if ok {
			entries = append(entries, e)
		}
	} else {
		files, err := collectFiles(paths, *sniffBytes, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "readtime: %v\n", err)
			return 1
		}
		if len(files) == 0 {
			fmt.Fprintln(stderr, "readtime: no text files found")
			return 1
		}
		for _, f := range files {
			raw, err := os.ReadFile(f.path)
			if err != nil {
				fmt.Fprintf(stderr, "readtime: %s: %v\n", f.path, err)
				continue
			}
			e, ok := analyze(f.display, raw, *includeDrafts, *wpm)
			if ok {
				entries = append(entries, e)
			}
		}
	}

	if len(entries) == 0 {
		fmt.Fprintln(stderr, "readtime: no text files found")
		return 1
	}

	sortEntries(entries, *sortKey)

	if *jsonOut {
		writeJSON(stdout, entries, *wpm)
	} else if len(entries) == 1 {
		writeDetail(stdout, entries[0])
	} else {
		writeTable(stdout, entries, *wpm)
	}
	return 0
}

// analyze parses raw and turns it into an entry, honoring the draft
// filter. ok is false if the post was excluded as a draft.
func analyze(display string, raw []byte, includeDrafts bool, wpm int) (entry, bool) {
	res := parse.Parse(raw)
	if res.Draft && !includeDrafts {
		return entry{}, false
	}
	return entry{
		Display: display,
		Title:   res.Title,
		Stats:   stats.Compute(res.Prose, wpm),
	}, true
}

// ---------------------------------------------------------------------------
// File discovery
// ---------------------------------------------------------------------------

type fileRef struct {
	path    string // path to read from disk
	display string // path shown in output, relative to the arg it came from
}

// collectFiles walks each argument path, gathering text files. Directories
// are walked recursively, skipping dotted directories and Hugo
// build/dependency output; a file (whether named directly or found while
// walking) is included if its content looks like text, regardless of
// extension.
func collectFiles(paths []string, sniffBytes int, stderr io.Writer) ([]fileRef, error) {
	var files []fileRef
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(stderr, "readtime: %s: %v\n", p, err)
			continue
		}
		if !info.IsDir() {
			ok, err := looksLikeText(p, sniffBytes)
			if err != nil {
				fmt.Fprintf(stderr, "readtime: %s: %v\n", p, err)
				continue
			}
			if !ok {
				fmt.Fprintf(stderr, "readtime: %s: not a text file, skipping\n", p)
				continue
			}
			files = append(files, fileRef{path: p, display: filepath.Base(p)})
			continue
		}
		base := p
		walkErr := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Fprintf(stderr, "readtime: %s: %v\n", path, err)
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if path != base && (strings.HasPrefix(name, ".") || skipDirs[name]) {
					return filepath.SkipDir
				}
				return nil
			}
			ok, err := looksLikeText(path, sniffBytes)
			if err != nil {
				fmt.Fprintf(stderr, "readtime: %s: %v\n", path, err)
				return nil
			}
			if !ok {
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				rel = path
			}
			files = append(files, fileRef{path: path, display: rel})
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return files, nil
}

// looksLikeText reports whether path's content appears to be text rather
// than binary. It reads up to sniffBytes from the start of the file and
// treats the presence of a NUL byte in that sample as proof of binary
// content: real-world binary formats (images, archives, executables) almost
// always have one within the first few hundred bytes, while text in any
// encoding -- UTF-8 or otherwise -- never contains one. This is the same
// heuristic git and the Unix `file` command use, and it avoids the false
// positives a printable-character or UTF-8-validity check would produce on
// legitimately non-UTF-8 text.
func looksLikeText(path string, sniffBytes int) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, sniffBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}
	return !bytes.Contains(buf[:n], []byte{0}), nil
}

// ---------------------------------------------------------------------------
// Sorting
// ---------------------------------------------------------------------------

func sortEntries(entries []entry, key string) {
	switch key {
	case "words":
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Stats.Words > entries[j].Stats.Words })
	case "time":
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].Stats.ReadingSeconds > entries[j].Stats.ReadingSeconds
		})
	case "grade":
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Stats.GradeLevel > entries[j].Stats.GradeLevel })
	default: // name
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Display < entries[j].Display })
	}
}

// ---------------------------------------------------------------------------
// Output: table
// ---------------------------------------------------------------------------

func writeTable(w io.Writer, entries []entry, wpm int) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%5s\t%5s\t%6s\t%5s\t%6s\n", "FILE", "WORDS", "SENT", "READ", "EASE", "GRADE")

	var totalWords, totalSentences, totalSyllables int
	for _, e := range entries {
		st := e.Stats
		fmt.Fprintf(tw, "%s\t%5d\t%5d\t%6s\t%5.1f\t%6.1f\n",
			e.Display, st.Words, st.Sentences, st.ReadingTime, st.ReadingEase, st.GradeLevel)
		totalWords += st.Words
		totalSentences += st.Sentences
		totalSyllables += st.Syllables
	}

	fmt.Fprintln(tw, "---")

	totals := stats.FromCounts(totalWords, totalSentences, totalSyllables, wpm)
	label := fmt.Sprintf("%d files", len(entries))
	if len(entries) == 1 {
		label = "1 file"
	}
	fmt.Fprintf(tw, "%s\t%5d\t%5d\t%6s\t%5.1f\t%6.1f\n",
		label, totals.Words, totals.Sentences, totals.ReadingTime, totals.ReadingEase, totals.GradeLevel)

	tw.Flush()
}

// writeDetail prints the single-file long-form view, which has room for
// the reading-ease band label.
func writeDetail(w io.Writer, e entry) {
	st := e.Stats
	fmt.Fprintln(w, e.Display)
	fmt.Fprintf(w, "  %-14s%d\n", "Words:", st.Words)
	fmt.Fprintf(w, "  %-14s%d\n", "Sentences:", st.Sentences)
	fmt.Fprintf(w, "  %-14s%s\n", "Reading time:", st.ReadingTime)
	fmt.Fprintf(w, "  %-14s%.1f (%s)\n", "Reading ease:", st.ReadingEase, st.ReadingEaseBand)
	fmt.Fprintf(w, "  %-14s%.1f\n", "Grade level:", st.GradeLevel)
}

// ---------------------------------------------------------------------------
// Output: JSON
// ---------------------------------------------------------------------------

type jsonEntry struct {
	File               string  `json:"file"`
	Title              string  `json:"title"`
	Words              int     `json:"words"`
	Sentences          int     `json:"sentences"`
	Syllables          int     `json:"syllables"`
	ReadingTimeSeconds int     `json:"reading_time_seconds"`
	ReadingTime        string  `json:"reading_time"`
	ReadingEase        float64 `json:"reading_ease"`
	ReadingEaseBand    string  `json:"reading_ease_band"`
	GradeLevel         float64 `json:"grade_level"`
}

type jsonTotals struct {
	Words              int     `json:"words"`
	Sentences          int     `json:"sentences"`
	Syllables          int     `json:"syllables"`
	ReadingTimeSeconds int     `json:"reading_time_seconds"`
	ReadingTime        string  `json:"reading_time"`
	ReadingEase        float64 `json:"reading_ease"`
	ReadingEaseBand    string  `json:"reading_ease_band"`
	GradeLevel         float64 `json:"grade_level"`
}

type jsonOutput struct {
	Files  []jsonEntry `json:"files"`
	Totals jsonTotals  `json:"totals"`
}

func toJSONEntry(e entry) jsonEntry {
	st := e.Stats
	return jsonEntry{
		File:               e.Display,
		Title:              e.Title,
		Words:              st.Words,
		Sentences:          st.Sentences,
		Syllables:          st.Syllables,
		ReadingTimeSeconds: st.ReadingSeconds,
		ReadingTime:        st.ReadingTime,
		ReadingEase:        st.ReadingEase,
		ReadingEaseBand:    st.ReadingEaseBand,
		GradeLevel:         st.GradeLevel,
	}
}

func writeJSON(w io.Writer, entries []entry, wpm int) {
	out := jsonOutput{Files: make([]jsonEntry, 0, len(entries))}
	var totalWords, totalSentences, totalSyllables int
	for _, e := range entries {
		out.Files = append(out.Files, toJSONEntry(e))
		totalWords += e.Stats.Words
		totalSentences += e.Stats.Sentences
		totalSyllables += e.Stats.Syllables
	}
	totals := stats.FromCounts(totalWords, totalSentences, totalSyllables, wpm)
	out.Totals = jsonTotals{
		Words:              totals.Words,
		Sentences:          totals.Sentences,
		Syllables:          totals.Syllables,
		ReadingTimeSeconds: totals.ReadingSeconds,
		ReadingTime:        totals.ReadingTime,
		ReadingEase:        totals.ReadingEase,
		ReadingEaseBand:    totals.ReadingEaseBand,
		GradeLevel:         totals.GradeLevel,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Errors here would only come from an unwritable stdout, which we can't
	// meaningfully recover from.
	_ = enc.Encode(out)
}
