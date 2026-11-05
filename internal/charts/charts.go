package charts

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"
)

// Bar is one horizontal mastery/comparison row.
type Bar struct {
	Label string
	Sub   string // e.g. "2 students · 3 evaluated"
	Pct   float64
	Extra string // right-aligned secondary value, e.g. "90% avg"
}

// ColorFor maps a percent to the token-based colors (explicit SVG fill attr).
func fillClass(pct float64) (fill, valCls string) {
	switch {
	case pct >= 75:
		return "#0F766E", "high" // success token
	case pct >= 60:
		return "#B45309", "mid" // warning token
	default:
		return "#DC2626", "low" // danger token
	}
}

// esc guards XML attribute/text nodes.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;")
	return r.Replace(s)
}

// Bars renders a responsive horizontal bar list. Fill and track colors are
// explicit SVG attributes from the design tokens; text lives in HTML for
// crisp rendering. ARIA labels duplicate the data.
func Bars(items []Bar) template.HTML {
	if len(items) == 0 {
		return `<p class="muted small">No evaluated data yet.</p>`
	}
	var b strings.Builder
	for _, it := range items {
		pct := it.Pct
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		cls, valCls := fillClass(pct)
		fmt.Fprintf(&b, `<div class="hbar-row" role="img" aria-label="%s: %.0f%%">`, esc(it.Label), pct)
		fmt.Fprintf(&b, `<div class="hbar-label">%s<span class="small">%s</span></div>`, esc(it.Label), esc(it.Sub))
		fmt.Fprintf(&b, `<div class="hbar-track"><svg viewBox="0 0 100 10" preserveAspectRatio="none" width="100%%" height="10" aria-hidden="true">`)
		fmt.Fprintf(&b, `<rect x="0" y="0" width="100" height="10" rx="5" fill="#E4E8F0"></rect><rect x="0" y="0" width="%.1f" height="10" rx="5" fill="%s"></rect></svg></div>`, pct, cls)
		fmt.Fprintf(&b, `<div class="hbar-value %s">%s</div></div>`, valCls, esc(it.Extra))
	}
	return template.HTML(b.String())
}

// Progress renders a small inline progress bar. Colors are explicit or
// attribute-explicit design tokens (#536FE5 fill on #E4E8F0 track) so no CSS
// class omission can ever paint browser-default black.
func Progress(pct float64) template.HTML {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return template.HTML(fmt.Sprintf(
		`<div class="row-progress" role="img" aria-label="%.0f%% complete"><svg viewBox="0 0 100 8" preserveAspectRatio="none" width="100%%" height="8" aria-hidden="true"><rect x="0" y="0" width="100" height="8" rx="4" fill="#E4E8F0"></rect><rect x="0" y="0" width="%.1f" height="8" rx="4" fill="#536FE5"></rect></svg></div>`, pct, pct))
}

// LinePt is one approved evaluation point on the trend line.
type LinePt struct {
	X int64 // unix seconds
	Y float64
}

// trimZero renders percents without decimals for labels.
func trimZero(v float64) string { return fmt.Sprintf("%.0f", v) }

// Line renders a chronological line chart with axes and point labels.
// Coincident evaluations (same UTC day) are grouped into a single point whose
// tooltip discloses how many evaluations it represents, so the chart never
// invents a chronology. With fewer than two distinct days it returns an
// honest empty-state explanation.
func Line(pts []LinePt) template.HTML {
	type dayAgg struct {
		X     int64
		Sum   float64
		Y     float64
		Count int
	}
	if len(pts) < 2 {
		if len(pts) == 1 {
			return `<p class="muted small">One approved evaluation so far — a trend line appears after a second approved result on a later day.</p>`
		}
		return `<p class="muted small">Not enough approved evaluations yet to draw a trend line. Once at least two approved results exist, the trend appears here.</p>`
	}
	byDay := map[int64]*dayAgg{}
	for _, p := range pts {
		day := (p.X / 86400) * 86400
		if day < 0 {
			day = 0
		}
		a := byDay[day]
		if a == nil {
			a = &dayAgg{X: day}
			byDay[day] = a
		}
		a.Sum += p.Y
		a.Count++
	}
	var days []*dayAgg
	for _, a := range byDay {
		a.Y = a.Sum / float64(a.Count)
		days = append(days, a)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].X < days[j].X })
	if len(days) < 2 {
		return `<p class="muted small">All approved evaluations share the same approval date, so there is no trend to display yet — the individual approved results are listed below.</p>`
	}

	W, H, padL, padB, padT := 640, 220, 44, 30, 14
	minX, maxX := days[0].X, days[len(days)-1].X
	spanX := float64(maxX - minX)
	if spanX <= 0 {
		spanX = 1
	}
	xy := func(i int) (float64, float64) {
		x := float64(days[i].X-minX)/spanX*float64(W-padL-28) + float64(padL)
		y := float64(H-padB) - days[i].Y/100*float64(H-padB-padT)
		return x, y
	}
	var b strings.Builder
	b.WriteString(`<div class="chart-wrap"><svg viewBox="0 0 640 220" width="100%" role="img" aria-label="Approved performance trend">`)
	b.WriteString(`<title>Approved performance trend</title><desc>Each point is the mean approved evaluation score in percent for that day.</desc>`)
	b.WriteString(`<rect x="0" y="0" width="640" height="220" fill="#FFFFFF"></rect>`)
	for _, gy := range []float64{0, 25, 50, 75, 100} {
		y := float64(H-padB) - gy/100*float64(H-padB-padT)
		b.WriteString(`<line x1="44" x2="612" stroke="#E4E8F0" stroke-width="1" y1="` + trimZero(y) + `" y2="` + trimZero(y) + `"></line>`)
		b.WriteString(`<text x="36" y="` + trimZero(y+3) + `" text-anchor="end" font-size="10" fill="#94A3B8">` + trimZero(gy) + `</text>`)
	}
	var poly, dots strings.Builder
	for i, a := range days {
		x, y := xy(i)
		if i > 0 {
			fmt.Fprintf(&poly, " L%.1f %.1f", x, y)
		} else {
			fmt.Fprintf(&poly, "M%.1f %.1f", x, y)
		}
		fmt.Fprintf(&dots, `<circle cx="%.1f" cy="%.1f" r="3.6" fill="#536FE5"><title>%s — %.0f%% (%d evaluation%s)</title></circle>`,
			x, y, timeDayLabel(a.X), a.Y, a.Count, plural(a.Count))
		fmt.Fprintf(&dots, `<text x="%.1f" y="%.1f" text-anchor="middle" font-size="9" fill="#64748B">%s</text>`, x, y-9, trimZero(a.Y))
	}
	b.WriteString(`<path d="` + poly.String() + `" fill="none" stroke="#536FE5" stroke-width="2.4" stroke-linejoin="round"></path>`)
	b.WriteString(dots.String())
	b.WriteString(`<text x="` + trimZero(float64(padL)) + `" y="212" font-size="10" fill="#94A3B8">approved evaluations over time →</text>`)
	b.WriteString(`</svg></div>`)
	return template.HTML(b.String())
}

// timeDayLabel renders a short date for the tooltip.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func timeDayLabel(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("02 Jan")
}
