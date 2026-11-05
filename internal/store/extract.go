package store

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pdfreader "github.com/ledongthuc/pdf"
)

// ExtractResult reports extracted text plus whether extraction was partial
// or failed, so the engine can flag review instead of pretending.
type ExtractResult struct {
	Text   string
	Failed bool
	Reason string
}

// maxTextBytes bounds extraction output to keep evaluation input sane.
const maxTextBytes = 512 * 1024

// ExtractFromBytes pulls text out of .txt, .docx and .pdf payloads. The first
// argument is any filename; its extension selects the extractor.
func ExtractFromBytes(filename string, data []byte) ExtractResult {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	switch ext {
	case "txt", "md":
		return extractTXT(data)
	case "docx":
		return extractDOCX(data)
	case "pdf":
		return extractPDF(data)
	default:
		return ExtractResult{Failed: true, Reason: "unsupported format for text extraction"}
	}
}

func extractTXT(data []byte) ExtractResult {
	if !utf8.Valid(data) {
		// A UTF-16BOM text or binary junk: reject rather than guess.
		if hasBOM(data) {
			return ExtractResult{Failed: true, Reason: "UTF-16 text files are not supported; please save as plain UTF-8"}
		}
		return ExtractResult{Failed: true, Reason: "file is not valid UTF-8 text"}
	}
	return ExtractResult{Text: truncate(data)}
}

func hasBOM(data []byte) bool {
	return len(data) > 1 && (data[0] == 0xFE && data[1] == 0xFF || data[0] == 0xFF && data[1] == 0xFE)
}

func truncate(data []byte) string {
	if len(data) > maxTextBytes {
		data = data[:maxTextBytes]
	}
	return string(data)
}

// extractDOCX walks word/document.xml via archive/zip and strips text runs.
func extractDOCX(data []byte) ExtractResult {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ExtractResult{Failed: true, Reason: "file is not a readable DOCX archive"}
	}
	var doc *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, "word/document.xml") {
			doc = f
			break
		}
	}
	if doc == nil {
		return ExtractResult{Failed: true, Reason: "DOCX archive is missing its document body"}
	}
	rc, err := doc.Open()
	if err != nil {
		return ExtractResult{Failed: true, Reason: "cannot open DOCX document body"}
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		return ExtractResult{Failed: true, Reason: "cannot read DOCX document body"}
	}
	text, err := docxText(string(raw))
	if err != nil {
		return ExtractResult{Failed: true, Reason: "DOCX content is malformed"}
	}
	if strings.TrimSpace(text) == "" {
		return ExtractResult{Failed: true, Reason: "DOCX contains no extractable text (image-only?)"}
	}
	return ExtractResult{Text: truncate([]byte(text))}
}

// docxText parses WML minimally: paragraphs -> newlines, tabs preserved.
func docxText(xmlStr string) (string, error) {
	type paragraph struct {
		Runs []string `xml:"any"`
	}
	// We deliberately parse with a token decoder rather than a struct to
	// tolerate the document.xml namespace soup.
	dec := xml.NewDecoder(strings.NewReader(xmlStr))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	var b strings.Builder
	depthText := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("decode: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				b.WriteString("\n")
			case "t", "instrText":
				depthText++
			case "tab":
				b.WriteString("\t")
			case "br":
				b.WriteString("\n")
			}
		case xml.CharData:
			if depthText > 0 {
				b.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t", "instrText":
				if depthText > 0 {
					depthText--
				}
			}
		}
	}
	_ = paragraph{}
	text := b.String()
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return text, nil
}

// extractPDF uses the pure-Go ledongthuc/pdf reader. Encrypted, image-only or
// exotic PDFs are reported as failed rather than faked.
func extractPDF(data []byte) ExtractResult {
	r := bytes.NewReader(data)
	pdfr, err := pdfreader.NewReader(r, r.Size())
	if err != nil {
		return ExtractResult{Failed: true, Reason: "file is not a readable PDF (or uses unsupported encryption)"}
	}
	var b strings.Builder
	pages := pdfr.NumPage()
	if pages == 0 {
		return ExtractResult{Failed: true, Reason: "PDF has no readable pages"}
	}
	if pages > 200 {
		pages = 200
	}
	for i := 1; i <= pages; i++ {
		p := pdfr.Page(i)
		if p.V.IsNull() {
			continue
		}
		txt, err := p.GetPlainText(nil)
		if err != nil {
			continue // skip unreadable page, keep going
		}
		b.WriteString(txt)
		b.WriteString("\n")
	}
	text := strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == '⊙' }), "\n")
	if strings.TrimSpace(text) == "" {
		return ExtractResult{Failed: true, Reason: "PDF contains no machine-readable text (image-only?)"}
	}
	return ExtractResult{Text: truncate([]byte(text))}
}
