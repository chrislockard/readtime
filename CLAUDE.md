# CLAUDE.md

## Working agreement

These are standing instructions for this repo. Follow them unless I say otherwise
in the moment.

- **Plan first, then act.** For anything beyond a trivial edit, tell me the
  approach before you start changing files. A short plan I can redirect beats a
  finished change I have to unwind.
- **Prefer Sonnet.** Use Sonnet for routine work in this repo. Reach for Opus
  only when a problem is genuinely hard, and say why you switched.
- **Never spawn subagents without asking.** Not for research, not for review, not
  for "it'll be faster in parallel." Ask, and wait for a yes.
- **Don't run the full test suite unless asked.** Run the specific package or
  test relevant to what you changed (`go test ./internal/stats/`). I'll ask for
  the whole suite when I want it.
- **Keep the work inexpensive, in tokens and dollars.** Read what you need, not
  the whole repo. Don't re-derive things already established in the conversation.
  Prefer one targeted edit over a broad refactor. If a task is about to get
  expensive, say so and let me decide.

## Project

`readtime` is a standalone CLI that analyzes Hugo markdown posts and reports word
count, sentence count, estimated reading time, Flesch Reading Ease, and
Flesch-Kincaid Grade Level.

It exists to be shared, so two constraints are load-bearing:

- **Standard library only.** No third-party dependencies. This keeps
  cross-compilation trivial and the binary self-contained.
- **Cross-platform.** It must build for macOS, Linux, and Windows. Use
  `path/filepath` for paths, never hardcoded separators.

Primary corpus for manual testing: `~/Workspace/chrislockard.net/content/post`
(132 posts, all YAML front matter; 119 non-draft).

## Commands

```sh
go build -o readtime .              # build
go test ./internal/stats/           # test one package (preferred)
gofmt -l . && go vet ./...          # must both be clean before committing

GOOS=linux   GOARCH=amd64 go build -o readtime-linux .
GOOS=windows GOARCH=amd64 go build -o readtime.exe .
```

## Layout

    main.go              flags, recursive file walking, sorting, output
    internal/parse/      markdown + Hugo stripping, down to bare prose
    internal/stats/      counting and readability math

## Correctness bar

The numbers this tool prints are the whole product. Statistics that look
plausible but are wrong are worse than an obvious crash, so changes to the
counting heuristics in `internal/stats` need a test case showing the specific
input they fix.

The two heuristics that feed both formulas — sentence boundaries and syllable
counts — are the fragile parts. Sentence count is a divisor; an error there
propagates into every score.
