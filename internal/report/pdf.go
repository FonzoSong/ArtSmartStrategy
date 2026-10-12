// Package report renders the team operations report as a real PDF document.
package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/go-pdf/fpdf"
)

// cjkFont is embedded so Chinese text renders in any deployment, including the
// distroless image, without depending on fonts installed on the reader's machine.
// The embedded face covers CJK glyphs only, so Latin text and digits are drawn
// with the built-in Helvetica font (see writeRuns).
//
//go:embed fonts/DroidSansFallbackFull.ttf
var cjkFont []byte

const (
	cjkFontName = "droidsansfallback"
	latinFont   = "Helvetica"

	marginX  = 14.0
	colSpan  = 182.0
	bodySize = 9.0

	// A4 portrait geometry: 297mm tall with 15mm margins plus the 20mm automatic
	// page-break margin, so a row must start above this limit.
	pageBottomLimit = 297.0 - 15.0 - 20.0
)

// Table is one section of the report.
type Table struct {
	Title   string
	Headers []string
	Rows    [][]string
	Note    string
}

// Document is the data a report needs.
type Document struct {
	TeamName    string
	GeneratedAt time.Time
	Range       string
	Filters     []string
	Summary     [][2]string
	Tables      []Table
	Footer      string
}

// cell is one table cell.
type cell struct {
	width  float64
	value  string
	align  string
	border bool
	fill   bool
	size   float64
}

// Render builds the PDF document and returns its bytes.
func Render(document Document) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginX, 15, marginX)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddUTF8FontFromBytes(cjkFontName, "", cjkFont)
	pdf.SetFooterFunc(func() { writeFooter(pdf) })
	pdf.AddPage()

	writeLine(pdf, colSpan, 11, "运营数据报告", "L", false, 17)
	writeLine(pdf, colSpan, 6, "团队："+document.TeamName, "L", false, 10)
	if document.Range != "" {
		writeLine(pdf, colSpan, 6, "统计范围："+document.Range, "L", false, 10)
	}
	if len(document.Filters) > 0 {
		writeLine(pdf, colSpan, 6, "筛选条件："+strings.Join(document.Filters, " · "), "L", false, 10)
	}
	writeLine(pdf, colSpan, 6, "生成时间："+document.GeneratedAt.Format("2006-01-02 15:04"), "L", false, 10)
	pdf.Ln(3)

	if len(document.Summary) > 0 {
		sectionTitle(pdf, "核心指标")
		for index, item := range document.Summary {
			writeRow(pdf, 7, cell{width: 60, value: item[0], border: true, fill: index%2 == 1, size: 10}, cell{width: colSpan - 60, value: item[1], border: true, fill: index%2 == 1, size: 10})
		}
		pdf.Ln(4)
	}
	for _, table := range document.Tables {
		sectionTitle(pdf, table.Title)
		renderTable(pdf, table)
		pdf.Ln(4)
	}
	if document.Footer != "" {
		writeParagraph(pdf, colSpan, 8, document.Footer)
	}

	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func sectionTitle(pdf *fpdf.Fpdf, title string) {
	writeLine(pdf, colSpan, 8, title, "L", false, 12)
}

func renderTable(pdf *fpdf.Fpdf, table Table) {
	if len(table.Headers) == 0 {
		return
	}
	width := colSpan / float64(len(table.Headers))
	header := make([]cell, 0, len(table.Headers))
	for _, value := range table.Headers {
		header = append(header, cell{width: width, value: value, align: "C", border: true, fill: true, size: bodySize})
	}
	pdf.SetFillColor(238, 236, 231)
	writeRow(pdf, 7, header...)
	for index, row := range table.Rows {
		cells := make([]cell, 0, len(table.Headers))
		for column := range table.Headers {
			value := ""
			if column < len(row) {
				value = row[column]
			}
			cells = append(cells, cell{width: width, value: value, border: true, fill: index%2 == 1, size: bodySize})
		}
		if index%2 == 1 {
			pdf.SetFillColor(248, 246, 241)
		}
		writeRow(pdf, 7, cells...)
	}
	if len(table.Rows) == 0 {
		writeRow(pdf, 7, cell{width: colSpan, value: "暂无数据", border: true, size: bodySize})
	}
	if table.Note != "" {
		writeParagraph(pdf, colSpan, 8, table.Note)
	}
}

// writeRow draws one row of cells and advances to the next row. It starts a new
// page when the row would not fit.
func writeRow(pdf *fpdf.Fpdf, height float64, cells ...cell) {
	if pdf.GetY()+height > pageBottomLimit {
		pdf.AddPage()
	}
	y := pdf.GetY()
	x := marginX
	for _, item := range cells {
		pdf.SetXY(x, y)
		if item.border {
			pdf.SetDrawColor(203, 199, 191)
			pdf.CellFormat(item.width, height, "", "1", 0, "", item.fill, 0, "")
		}
		writeRuns(pdf, item, x, y, height)
		x += item.width
	}
	pdf.SetXY(marginX, y+height)
	pdf.SetFillColor(255, 255, 255)
}

// writeLine writes a full-width line of text without a border.
func writeLine(pdf *fpdf.Fpdf, width, height float64, value, align string, fill bool, size float64) {
	writeRow(pdf, height, cell{width: width, value: value, align: align, fill: fill, size: size})
}

// textRun is a piece of a string drawn with one font.
type textRun struct {
	text  string
	ascii bool
}

// splitRuns separates Latin (drawable with the core font) from the rest.
func splitRuns(value string) []textRun {
	runs := make([]textRun, 0, 4)
	var current strings.Builder
	mode := true
	started := false
	for _, symbol := range value {
		isASCII := symbol < unicode.MaxASCII
		if started && isASCII != mode {
			runs = append(runs, textRun{text: current.String(), ascii: mode})
			current.Reset()
		}
		mode = isASCII
		started = true
		current.WriteRune(symbol)
	}
	if current.Len() > 0 {
		runs = append(runs, textRun{text: current.String(), ascii: mode})
	}
	return runs
}

func setRunFont(pdf *fpdf.Fpdf, run textRun, size float64) {
	if run.ascii {
		pdf.SetFont(latinFont, "", size)
		return
	}
	pdf.SetFont(cjkFontName, "", size)
}

func runWidth(pdf *fpdf.Fpdf, run textRun, size float64) float64 {
	setRunFont(pdf, run, size)
	return pdf.GetStringWidth(run.text)
}

func measureRuns(pdf *fpdf.Fpdf, runs []textRun, size float64) float64 {
	total := 0.0
	for _, run := range runs {
		total += runWidth(pdf, run, size)
	}
	return total
}

// writeRuns draws the cell text on top of its box, switching fonts per run.
func writeRuns(pdf *fpdf.Fpdf, item cell, x, y, height float64) {
	if item.value == "" {
		return
	}
	runs := fitRuns(pdf, splitRuns(item.value), item.width-3, item.size)
	total := measureRuns(pdf, runs, item.size)
	offset := 1.5
	switch item.align {
	case "C":
		offset = (item.width - total) / 2
	case "R":
		offset = item.width - total - 1.5
	}
	if offset < 1 {
		offset = 1
	}
	pdf.SetXY(x+offset, y)
	for _, run := range runs {
		setRunFont(pdf, run, item.size)
		pdf.CellFormat(runWidth(pdf, run, item.size), height, run.text, "", 0, "", false, 0, "")
	}
}

// fitRuns truncates text so it stays inside the cell.
func fitRuns(pdf *fpdf.Fpdf, runs []textRun, maxWidth, size float64) []textRun {
	if measureRuns(pdf, runs, size) <= maxWidth {
		return runs
	}
	runs = append(runs, textRun{text: "…"})
	for measureRuns(pdf, runs, size) > maxWidth {
		last := len(runs) - 1
		trimmed := []rune(runs[last].text)
		switch {
		case len(trimmed) > 1:
			runs[last].text = string(trimmed[:len(trimmed)-1])
		case last > 0:
			runs = runs[:last]
		default:
			return []textRun{}
		}
	}
	return runs
}

// writeParagraph wraps mixed-script text into the available width.
func writeParagraph(pdf *fpdf.Fpdf, width, size float64, value string) {
	height := size*0.45 + 2.5
	var line strings.Builder
	flush := func() {
		if line.Len() == 0 {
			return
		}
		writeRow(pdf, height, cell{width: width, value: line.String(), size: size})
		line.Reset()
	}
	for _, symbol := range strings.TrimSpace(value) {
		candidate := line.String() + string(symbol)
		if measureRuns(pdf, splitRuns(candidate), size) > width && line.Len() > 0 {
			flush()
			continue
		}
		line.WriteRune(symbol)
	}
	flush()
}

// writeFooter centres the page number and never triggers a page break.
func writeFooter(pdf *fpdf.Fpdf) {
	label := fmt.Sprintf("第 %d 页", pdf.PageNo())
	total := 0.0
	runs := splitRuns(label)
	for _, run := range runs {
		total += runWidth(pdf, run, 8)
	}
	pdf.SetXY(marginX+(colSpan-total)/2, -16)
	for _, run := range runs {
		setRunFont(pdf, run, 8)
		pdf.CellFormat(runWidth(pdf, run, 8), 6, run.text, "", 0, "", false, 0, "")
	}
}
