package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLooksLikeText(t *testing.T) {
	dir := t.TempDir()

	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("just some plain prose, no markup here."), 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "image.png")
	if err := os.WriteFile(binPath, []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"plain text", textPath, true},
		{"binary with early NUL byte", binPath, false},
		{"empty file", emptyPath, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := looksLikeText(tt.path, 512)
			if err != nil {
				t.Fatalf("looksLikeText(%q): %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("looksLikeText(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestLooksLikeText_SniffWindow verifies that a NUL byte past the sniff
// window is invisible to the check: sniffBytes governs where the sample
// ends, so a small enough window makes a binary file look like text.
func TestLooksLikeText_SniffWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed")
	content := append([]byte(strings.Repeat("a", 16)), 0x00, 'b')
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := looksLikeText(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Errorf("looksLikeText with an 8-byte window before the NUL byte = false, want true")
	}

	got, err = looksLikeText(path, 512)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Errorf("looksLikeText with a window covering the NUL byte = true, want false")
	}
}

func TestCollectFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles := map[string][]byte{
		"post.md":            []byte("# Hello\n\nSome prose."),
		"README":             []byte("no extension, still text"),
		"notes.log":          []byte("plain text with an unusual extension"),
		"photo.jpg":          {0xFF, 0xD8, 0xFF, 0x00, 0x10},
		"public/ignored.md":  []byte("# should be skipped, build output dir"),
		".hidden/ignored.md": []byte("# should be skipped, dotted dir"),
		"sub/nested.txt":     []byte("nested prose"),
	}
	for rel, content := range writeFiles {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stderr strings.Builder
	files, err := collectFiles([]string{dir}, 512, &stderr)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]bool)
	for _, f := range files {
		got[filepath.ToSlash(f.display)] = true
	}
	want := []string{"post.md", "README", "notes.log", filepath.ToSlash(filepath.Join("sub", "nested.txt"))}
	for _, w := range want {
		if !got[w] {
			t.Errorf("collectFiles: missing expected text file %q, got %v", w, got)
		}
	}
	unwanted := []string{"photo.jpg", filepath.ToSlash(filepath.Join("public", "ignored.md")), filepath.ToSlash(filepath.Join(".hidden", "ignored.md"))}
	for _, u := range unwanted {
		if got[u] {
			t.Errorf("collectFiles: unexpectedly included %q", u)
		}
	}
	if len(files) != len(want) {
		t.Errorf("collectFiles: got %d files %v, want %d", len(files), got, len(want))
	}
}

func TestCollectFiles_ExplicitBinaryArgSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	files, err := collectFiles([]string{path}, 512, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("collectFiles: expected explicit binary file argument to be skipped, got %v", files)
	}
	if !strings.Contains(stderr.String(), "not a text file") {
		t.Errorf("collectFiles: expected a skip warning on stderr, got %q", stderr.String())
	}
}
