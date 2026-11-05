package handlers

import (
	"html/template"
	"strings"
	"testing"
)

// Regression tests for the Phase 5 correction pass: chart/SVG markup must be
// rendered as trusted markup while any user-provided content stays escaped.

func TestProgressFuncIsTrustedHTML(t *testing.T) {
	tpl, err := template.New("p").Funcs(templateFuncs()).Parse(`{{progress 62}}`)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, nil); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, "<svg") || !strings.Contains(out, "<rect") {
		t.Fatalf("progress was HTML-escaped in page render: %q", out)
	}
	if strings.Contains(out, "&lt;") {
		t.Fatalf("markup double-escaped: %q", out)
	}
	if !strings.Contains(out, `fill="#536FE5"`) || !strings.Contains(out, `fill="#E4E8F0"`) {
		t.Fatalf("token colors missing: %q", out)
	}
	if !strings.Contains(out, `width="62.0"`) {
		t.Fatalf("width not scaled: %q", out)
	}
}

func TestUserContentStaysEscaped(t *testing.T) {
	// html/template must escape plain strings even mixed with trusted markup:
	tpl, err := template.New("e").Funcs(templateFuncs()).Parse(
		`<div>{{progress 50}}</div><span>{{.}}</span><span>{{initials .}}</span>`)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, "<script>alert(1) & \"quotes\"</script>"); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if strings.Contains(out, "<script>") {
		t.Fatalf("untrusted content rendered raw: %q", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") || !strings.Contains(out, "&#34;quotes&#34;") {
		t.Fatalf("escaping missing: %q", out)
	}
	// Trusted chart markup still intact next to escaped content.
	if !strings.Contains(out, "<svg") {
		t.Fatalf("trusted chart markup lost: %q", out)
	}
}

func TestInitialsSafe(t *testing.T) {
	tpl, err := template.New("t").Funcs(templateFuncs()).Parse(`{{initials .}}`)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, "<img src=x onerror=alert(1)> Ni"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "<img") {
		t.Fatalf("initials leaked markup: %q", sb.String())
	}
}
