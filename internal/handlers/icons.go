package handlers

import "html/template"

// iconPaths maps a name to the inner SVG path data (Lucide icons, 24×24,
// stroke-based). Only a curated set is needed; inline rendering keeps the
// dependency surface at zero.
var iconPaths = map[string]string{
	"layout":         `<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>`,
	"book-open":      `<path d="M12 7v14"/><path d="M3 18a1 1 0 0 0-1 1v-13a2 2 0 0 1 2-2h7v18z"/><path d="M21 18a1 1 0 0 0 1 1v-13a2 2 0 0 0-2-2h-7v18z"/>`,
	"clipboard-list": `<rect width="8" height="4" x="8" y="2" rx="1" ry="1"/><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><path d="M12 11h4"/><path d="M12 16h4"/><path d="M8 11h.01"/><path d="M8 16h.01"/>`,
	"users":          `<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>`,
	"check-circle":   `<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>`,
	"bar-chart":      `<path d="M3 3v16a2 2 0 0 0 2 2h16"/><path d="M7 16v-3"/><path d="M12 16V8"/><path d="M17 16v-6"/>`,
	"alert-triangle": `<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/>`,
	"clock":          `<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>`,
	"file-text":      `<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v5h5"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>`,
	"plus":           `<path d="M5 12h14"/><path d="M12 5v14"/>`,
	"log-out":        `<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>`,
	"trending-up":    `<path d="M16 7h6v6"/><path d="m22 7-8.5 8.5-5-5L2 17"/><path d="M16 17h6v6"/>`,
	"trending-down":  `<path d="M16 17h6v-6"/><path d="m22 17-8.5-8.5-5 5L2 7"/><path d="M16 7h4"/>`,
	"target":         `<circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/>`,
	"search":         `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`,
	"calendar":       `<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>`,
	"award":          `<circle cx="12" cy="8" r="6"/><path d="M15.477 12.89 17 22l-5-3-5 3 1.523-9.11"/>`,
	"activity":       `<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>`,
	"database":       `<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/>`,
	"sparkles":       `<path d="m12 3-1.9 5.8a2 2 0 0 1-1.3 1.3L3 12l5.8 1.9a2 2 0 0 1 1.3 1.3L12 21l1.9-5.8a2 2 0 0 1 1.3-1.3L21 12l-5.8-1.9a2 2 0 0 1-1.3-1.3Z"/>`,
	"hourglass":      `<path d="M5 22h14"/><path d="M5 2h14"/><path d="M17 22v-4.172a2 2 0 0 0-.586-1.414L12 12l-4.414-4.414A2 2 0 0 1 7 6.172V2"/><path d="M7 2v4.172a2 2 0 0 0 .586 1.414L12 12l4.414 4.414A2 2 0 0 1 17 17.822V22"/>`,
	"arrow-right":    `<path d="M5 12h14"/><path d="m12 5 7 7-7 7"/>`,
	"college":        `<path d="M22 10v6M2 10l10-5 10 5-10 5z"/><path d="M6 12v5c3 3 9 3 12 0v-5"/>`,
	"inbox":          `<path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>`,
}

// icon renders an inline Lucide-style stroke SVG.
func icon(name string, extra ...string) template.HTML {
	path, ok := iconPaths[name]
	if !ok {
		path = iconPaths["circle"]
	}
	cls := "ic"
	if len(extra) > 0 {
		cls = extra[0]
	}
	return template.HTML(`<svg class="` + cls + `" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + path + `</svg>`)
}
