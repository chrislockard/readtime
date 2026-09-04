package parse

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantProse string
		wantTitle string
		wantDraft bool
	}{
		{
			name:      "yaml front matter with title and draft",
			in:        "---\ntitle: \"Hello World\"\ndraft: true\n---\nSome text here.\n",
			wantProse: "Some text here.",
			wantTitle: "Hello World",
			wantDraft: true,
		},
		{
			name:      "yaml front matter draft false default",
			in:        "---\ntitle: Plain Title\n---\nBody text.\n",
			wantProse: "Body text.",
			wantTitle: "Plain Title",
			wantDraft: false,
		},
		{
			name:      "toml front matter",
			in:        "+++\ntitle = \"A TOML Post\"\ndraft = true\n+++\nToml body.\n",
			wantProse: "Toml body.",
			wantTitle: "A TOML Post",
			wantDraft: true,
		},
		{
			name:      "json front matter",
			in:        "{\n  \"title\": \"JSON Post\",\n  \"draft\": true\n}\nJson body.\n",
			wantProse: "Json body.",
			wantTitle: "JSON Post",
			wantDraft: true,
		},
		{
			name:      "no front matter",
			in:        "Just a plain paragraph.\n",
			wantProse: "Just a plain paragraph.",
		},
		{
			name:      "fenced code block removed",
			in:        "Before.\n\n```go\nfunc main() {}\n```\n\nAfter.\n",
			wantProse: "Before. After.",
		},
		{
			name:      "longer fence honored with info string",
			in:        "Text.\n\n````markdown\n```\nnested\n```\n````\n\nMore text.\n",
			wantProse: "Text. More text.",
		},
		{
			name:      "tilde fence removed",
			in:        "Before.\n\n~~~\ncode here\n~~~\n\nAfter.\n",
			wantProse: "Before. After.",
		},
		{
			name:      "indented code block removed",
			in:        "Paragraph.\n\n    indented code line\n    more code\n\nAfter paragraph.\n",
			wantProse: "Paragraph. After paragraph.",
		},
		{
			name:      "inline code span replaced with placeholder",
			in:        "Run `go build` to compile.\n",
			wantProse: "Run code to compile.",
		},
		{
			name:      "double backtick code span with literal backtick inside",
			in:        "Use ``code with ` backtick`` here.\n",
			wantProse: "Use code here.",
		},
		{
			name:      "self-closing angle shortcode removed",
			in:        "Look at this.\n\n{{< picture src=\"/x.png\" alt=\"x\" >}}\n\nNeat.\n",
			wantProse: "Look at this. Neat.",
		},
		{
			name:      "self-closing percent shortcode removed",
			in:        "See [this post]({{% relref \"/post/x.md\" %}}) for more.\n",
			wantProse: "See this post for more.",
		},
		{
			name:      "paired shortcode keeps inner prose",
			in:        "{{< callout title=\"Note\" >}}\nThis is important prose.\n{{< /callout >}}\n",
			wantProse: "This is important prose.",
		},
		{
			name:      "paired code shortcode drops content",
			in:        "Before.\n\n{{< highlight go >}}\nfunc main() {}\n{{< /highlight >}}\n\nAfter.\n",
			wantProse: "Before. After.",
		},
		{
			name:      "void shortcode with no closing tag removed entirely",
			in:        "Watch this.\n\n{{< youtube VVJldn_MmMY >}}\n\nCool video.\n",
			wantProse: "Watch this. Cool video.",
		},
		{
			name:      "multiline self-closing shortcode removed",
			in:        "Photo below.\n\n{{< picture\n  src=\"/x.jpg\"\n  alt=\"a photo\"\n>}}\n\nCaption text.\n",
			wantProse: "Photo below. Caption text.",
		},
		{
			name:      "image dropped entirely",
			in:        "Before ![alt text](/img.png) after.\n",
			wantProse: "Before after.",
		},
		{
			name:      "link keeps text drops url",
			in:        "Read the [documentation](https://example.com/docs) online.\n",
			wantProse: "Read the documentation online.",
		},
		{
			name:      "reference definition dropped",
			in:        "See [the link][1] for more.\n\n[1]: http://example.com \"Title\"\n",
			wantProse: "See [the link][1] for more.",
		},
		{
			name:      "autolink dropped",
			in:        "Visit <https://example.com> today.\n",
			wantProse: "Visit today.",
		},
		{
			name:      "bare url dropped",
			in:        "Visit https://example.com/page today.\n",
			wantProse: "Visit today.",
		},
		{
			name:      "html tags stripped keep text",
			in:        "This is <b>bold</b> and <em>italic</em> text.\n",
			wantProse: "This is bold and italic text.",
		},
		{
			name:      "script content dropped entirely",
			in:        "Before.\n<script>var x = 1;</script>\nAfter.\n",
			wantProse: "Before. After.",
		},
		{
			name:      "heading markers stripped keep text",
			in:        "# A Big Heading\n\nBody text.\n",
			wantProse: "A Big Heading Body text.",
		},
		{
			name:      "list bullets stripped",
			in:        "- first item\n- second item\n* third item\n",
			wantProse: "first item second item third item",
		},
		{
			name:      "ordered list markers stripped",
			in:        "1. first\n2) second\n",
			wantProse: "first second",
		},
		{
			name:      "blockquote markers stripped",
			in:        "> a quoted line\n>> nested quote\n",
			wantProse: "a quoted line nested quote",
		},
		{
			name:      "table pipes and separator row stripped",
			in:        "| Col A | Col B |\n| --- | --- |\n| val1 | val2 |\n",
			wantProse: "Col A Col B val1 val2",
		},
		{
			name:      "thematic break dropped",
			in:        "Above.\n\n---\n\nBelow.\n",
			wantProse: "Above. Below.",
		},
		{
			name:      "emphasis markers stripped keep words",
			in:        "This is **bold**, *italic*, and ~~struck~~ text.\n",
			wantProse: "This is bold, italic, and struck text.",
		},
		{
			name:      "footnote reference dropped definition text kept",
			in:        "A claim[^1] needs support.\n\n[^1]: Here is the footnote text.\n",
			wantProse: "A claim needs support. Here is the footnote text.",
		},
		{
			name:      "collapses excess whitespace",
			in:        "Line one.\n\n\n\nLine   two.\n",
			wantProse: "Line one. Line two.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse([]byte(tc.in))
			if got.Prose != tc.wantProse {
				t.Errorf("Prose = %q, want %q", got.Prose, tc.wantProse)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Draft != tc.wantDraft {
				t.Errorf("Draft = %v, want %v", got.Draft, tc.wantDraft)
			}
		})
	}
}

// Regression: a bare "{" prefix was treated as JSON front matter, so a post
// opening with a shortcode had that line eaten and the rest of its shortcodes
// mispaired -- which leaked code bodies into the prose.
func TestParseShortcodeAtStartIsNotJSONFrontMatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "leading shortcode is prose, not front matter",
			in:   "{{< picture foo >}}\nReal prose follows here.\n",
			want: "Real prose follows here.",
		},
		{
			name: "paired code shortcode at start still drops its body",
			in:   "{{< highlight go >}}\nfunc f() {}\n{{< /highlight >}}\nProse after.\n",
			want: "Prose after.",
		},
		{
			name: "genuine JSON front matter is still recognized",
			in:   "{\n  \"title\": \"Hello\"\n}\nProse body.\n",
			want: "Prose body.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse([]byte(tt.in)).Prose; got != tt.want {
				t.Errorf("Prose = %q, want %q", got, tt.want)
			}
		})
	}
}
