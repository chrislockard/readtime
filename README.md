# readtime

`readtime` is a command-line tool that analyzes Hugo blog posts (markdown)
and reports word count, sentence count, estimated reading time, and
readability scores. It's built entirely on the Go standard library, so it
has no third-party dependencies and cross-compiles cleanly for any platform
Go supports.

## Install / build

```sh
go build -o readtime .
```

Requires Go 1.27.1 or later (see `go.mod`).

### Cross-compiling

Since `readtime` only uses the standard library, cross-compilation is just
setting `GOOS`/`GOARCH`:

```sh
GOOS=windows GOARCH=amd64 go build -o readtime.exe .
GOOS=linux   GOARCH=amd64 go build -o readtime-linux .
GOOS=linux   GOARCH=arm64 go build -o readtime-linux-arm64 .
GOOS=darwin  GOARCH=arm64 go build -o readtime-mac .
```

## Usage

```sh
readtime [flags] <path>...
```

Each `path` may be a markdown file or a directory. Directories are walked
recursively for `.md` and `.markdown` files. Dotted directories (`.git`,
`.hugo_build.lock`'s parent, etc.) and Hugo's `public/`, `resources/`, and
`node_modules/` directories are skipped, since they're build output or
dependencies rather than content.

If no paths are given, `readtime` reads a single markdown document from
stdin and reports it as `(stdin)`.

```sh
readtime content/post                    # analyze a whole section
readtime content/post/my-post.md         # analyze a single post
cat content/post/my-post.md | readtime   # analyze from stdin
```

### Flags

| Flag       | Default | Description |
|------------|---------|-------------|
| `--json`   | off     | Emit JSON instead of the table. |
| `--wpm`    | `200`   | Reading speed in words per minute, used to estimate reading time. (Hugo itself defaults to 213 wpm.) |
| `--sort`   | `name`  | Sort order: `name`, `words`, `time`, or `grade`. `name` sorts ascending; the numeric keys sort descending, so the longest/hardest posts show up first. |
| `--drafts` | off     | Include posts with `draft: true` in front matter. Drafts are excluded by default. |
| `--version`| —       | Print the version and exit. |

Exit status is `0` on success, `1` on a usage error or if no markdown files
were found. A file that fails to read is reported to stderr and skipped;
it doesn't abort the rest of the run.

## Output

By default, analyzing more than one file prints a table, aligned with
`text/tabwriter`, with file paths shown relative to the argument you passed:

```
FILE                        WORDS  SENT   READ  EASE  GRADE
post/welcome-all.md           412    24  1m56s  62.4    8.9
post/on-hugo-themes.md       1893   102  8m52s  55.1   10.4
---
2 files                      2305   126 10m48s  58.8    9.7
```

The totals row aggregates word/sentence/syllable counts across every file
and *recomputes* reading time and readability scores from those sums —
it's not an average of the individual scores.

Analyzing exactly one file (or reading from stdin) switches to a detail
view, which has room to show the reading-ease band label:

```
post/welcome-all.md
  Words:        412
  Sentences:     24
  Reading time: 1m56s
  Reading ease: 62.4 (Standard)
  Grade level:  8.9
```

### JSON

`--json` emits a JSON object with a `files` array (one entry per analyzed
post) and a `totals` object with the same shape, aggregated the same way as
the table's totals row. Output is indented two spaces so it's readable
without piping through `jq`:

```json
{
  "files": [
    {
      "file": "post/welcome-all.md",
      "title": "Welcome, All",
      "words": 412,
      "sentences": 24,
      "syllables": 610,
      "reading_time_seconds": 116,
      "reading_time": "1m56s",
      "reading_ease": 62.4,
      "reading_ease_band": "Standard",
      "grade_level": 8.9
    }
  ],
  "totals": { "...": "..." }
}
```

## What the scores mean

- **Words** — whitespace-separated tokens containing at least one letter or
  digit. Hyphenated compounds (`well-known`) count as a single word; an em
  dash between two words with no surrounding space (`wait—really`) counts
  as two.
- **Sentences** — counted from runs of terminal punctuation (`.`, `!`,
  `?`), while ignoring common abbreviations (Mr., Dr., etc., e.g., a.m.,
  U.S., ...), single-letter initials (`C. S. Lewis`), decimals (`3.14`),
  and ellipses (which count as a single break, not three). A trailing
  chunk of text with no terminal punctuation still counts as one sentence.
- **Reading time** — `words / wpm` minutes, formatted as e.g. `1m56s` or,
  for sub-minute posts, `48s`. Never shown as `0s` for a non-empty post.
- **Flesch Reading Ease** — `206.835 - 1.015*(words/sentences) -
  84.6*(syllables/words)`, clamped to `[0, 100]`. Higher is easier to
  read. The table/detail view labels the score with its standard band:

  | Score  | Band              |
  |--------|-------------------|
  | 90+    | Very Easy         |
  | 80-89  | Easy              |
  | 70-79  | Fairly Easy       |
  | 60-69  | Standard          |
  | 50-59  | Fairly Difficult  |
  | 30-49  | Difficult         |
  | <30    | Very Confusing    |

- **Flesch-Kincaid Grade Level** — `0.39*(words/sentences) +
  11.8*(syllables/words) - 15.59`, floored at 0. Roughly, the U.S. school
  grade level needed to understand the text on a first read.

Syllable counts (which feed both readability scores) are estimated with a
heuristic: count vowel groups per word, drop a silent trailing `e` (except
after a consonant + `le`, as in "table"), and adjust for `-es`/`-ed`
suffixes that don't usually add a syllable of their own. It's a heuristic,
not a dictionary lookup, so expect it to be approximate on unusual words.

## Parsing

`readtime` reduces each post to plain prose before counting anything,
handling (in order): YAML/TOML/JSON front matter, fenced and indented code
blocks and inline code spans, this site's Hugo shortcodes (`picture`,
`relref`, `callout`, `youtube`, `highlight`, `x`, `paragame`, plus any other
shortcode using the same tag syntax), links and images, raw HTML, and
remaining markdown syntax (headings, lists, blockquotes, tables, thematic
breaks, emphasis markers, and footnotes).

Front matter is also where `title` and `draft` are read from, to label
output and to decide whether a post should be included by default.
