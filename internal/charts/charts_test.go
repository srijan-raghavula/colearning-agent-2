package charts

import (
	"strings"
	"testing"
)

func TestProgressWidthsAndColors(t *testing.T) {
	cases := map[float64]string{
		0:      `width="0.0" height="8" rx="4" fill="#536FE5"`,
		50:     `width="50.0" height="8" rx="4" fill="#536FE5"`,
		100:    `width="100.0" height="8" rx="4" fill="#536FE5"`,
		26.666: `width="26.7"`,
	}
	for pct, want := range cases {
		out := string(Progress(pct))
		if !strings.Contains(out, want) {
			t.Errorf("pct %.1f: missing %q in %s", pct, want, out)
		}
		if !strings.Contains(out, `fill="#E4E8F0"`) {
			t.Errorf("pct %.1f: track color missing", pct)
		}
		if !strings.Contains(out, "<rect") || !strings.Contains(out, "<svg") {
			t.Errorf("pct %.1f: not SVG", pct)
		}
		if strings.Contains(out, "fill=\"#000000\"") || strings.Contains(out, `class="fill-blue"`) {
			t.Errorf("pct %.1f: browser-default fill present", pct)
		}
	}
	// clamped
	if !strings.Contains(string(Progress(140)), `width="100.0"`) {
		t.Errorf("over 100 not clamped")
	}
	if !strings.Contains(string(Progress(-5)), `width="0.0"`) {
		t.Errorf("negative not clamped")
	}
}

func TestBarsTokenColorsAndEscaping(t *testing.T) {
	out := string(Bars([]Bar{
		{Label: "<b>Injection &</b> label", Sub: "meta \" quoted", Pct: 50, Extra: "50%"},
		{Label: "Strong", Pct: 82},
		{Label: "Weak", Pct: 61},
		{Label: "Crisis", Pct: 0},
		{Label: "Overflow", Pct: 250},
	}))
	// tokens: 50 → warning, 82 → success, 61 → warning, 0 → danger, 250 → clamped 100/success
	for _, want := range []string{
		`fill="#B45309"`, `fill="#0F766E"`, `fill="#DC2626"`, `fill="#E4E8F0"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing token fill %s", want)
		}
	}
	if strings.Contains(out, "fill=\"black\"") || strings.Contains(out, `fill-high`) {
		t.Errorf("browser-default fills leaked: %s", out)
	}
	if strings.Contains(out, "<b>Injection") {
		t.Errorf("label not escaped: %s", out)
	}
	if !strings.Contains(out, "&lt;b&gt;Injection &amp;&lt;/b&gt; label") {
		t.Errorf("escaping broken")
	}
	if strings.Contains(out, `width="250`) {
		t.Errorf("overflow width not clamped")
	}
}

func TestLineGroupingIdenticalTimestamps(t *testing.T) {
	// Exact anomaly from the showcase: three approvals on the same timestamp.
	base := int64(1_760_000_000)
	same := []LinePt{{X: base, Y: 70}, {X: base, Y: 70}, {X: base, Y: 70}}
	out := string(Line(same))
	if !strings.Contains(out, "share the same approval date") {
		t.Fatalf("identical-timestamp empty state missing: %s", out)
	}
	if strings.Contains(out, `<path d=`) {
		t.Errorf("trend implies a chronology that does not exist")
	}

	// two distinct days → grouped, count disclosed, polyline drawn
	mixed := []LinePt{{X: base, Y: 78}, {X: base, Y: 78}, {X: base + 86400, Y: 84}}
	out2 := string(Line(mixed))
	if !strings.Contains(out2, "<path d=") {
		t.Fatalf("valid two-day trend missing polyline")
	}
	if !strings.Contains(out2, "(2 evaluations)") || !strings.Contains(out2, "(1 evaluation)") {
		t.Errorf("grouping count not disclosed: %s", out2)
	}
}

func TestLineEmptyStates(t *testing.T) {
	if out := string(Line(nil)); !strings.Contains(out, "Not enough approved evaluations") {
		t.Errorf("nil trend empty state missing")
	}
	one := string(Line([]LinePt{{X: 1000, Y: 50}}))
	if !strings.Contains(one, "trend line appears after") {
		t.Errorf("single point explanation missing: %s", one)
	}
}
