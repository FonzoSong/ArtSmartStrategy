package report

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func sampleDocument() Document {
	return Document{
		TeamName:    "云锦戏曲社",
		GeneratedAt: time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC),
		Range:       "近 30 天",
		Filters:     []string{"近 30 天", "平台：视频号"},
		Summary: [][2]string{
			{"已发布内容（条）", "12"},
			{"总播放量", "48210"},
			{"平均看完比例", "63.42%"},
		},
		Tables: []Table{
			{Title: "平台表现", Headers: []string{"平台", "发布数", "播放量"}, Rows: [][]string{{"视频号", "7", "31200"}, {"内容社区C", "5", "17010"}}},
			{Title: "工作流分布", Headers: []string{"内容分类", "数量"}, Rows: [][]string{{"待制作", "3"}, {"未发布", "4"}}},
			{Title: "无数据表格", Headers: []string{"项目", "数量"}},
		},
		Footer: "指标口径：仅统计已发布内容。",
	}
}

func TestRenderProducesARealPDFDocument(t *testing.T) {
	content, err := Render(sampleDocument())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		t.Fatalf("document does not start with a PDF header: %q", string(content[:8]))
	}
	if !bytes.Contains(content, []byte("%%EOF")) {
		t.Fatal("document has no PDF trailer")
	}
	if len(content) < 1000 {
		t.Fatalf("document is suspiciously small: %d bytes", len(content))
	}
	if !bytes.Contains(content, []byte("/FontFile2")) {
		t.Fatal("the CJK font is not embedded, so Chinese text may not render")
	}
	if bytes.Contains(content, []byte("export.json")) {
		t.Fatal("the renderer must not emit a JSON payload")
	}
}

func TestRenderPaginatesLongReports(t *testing.T) {
	document := sampleDocument()
	table := Table{Title: "长表格", Headers: []string{"序号", "内容", "播放量"}}
	for index := 0; index < 400; index++ {
		table.Rows = append(table.Rows, []string{"行", "内容标题", "1000"})
	}
	document.Tables = []Table{table}
	content, err := Render(document)
	if err != nil {
		t.Fatal(err)
	}
	if count := bytes.Count(content, []byte("/Type /Page\n")); count < 2 {
		// The exact object spelling varies; fall back to counting page objects.
		if count = bytes.Count(content, []byte("/Type /Page")); count < 3 {
			t.Fatalf("expected a multi-page document, found %d page objects", count)
		}
	}
}

// TestRenderWritesSampleForInspection keeps a rendered sample available for
// external checks (pdftotext / pdftoppm) when PDFTOOL_SAMPLE is set.
func TestRenderWritesSampleForInspection(t *testing.T) {
	target := os.Getenv("PDFTOOL_SAMPLE")
	if target == "" {
		t.Skip("PDFTOOL_SAMPLE not set")
	}
	content, err := Render(sampleDocument())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(target, ".pdf") {
		t.Fatal("sample must be written with a .pdf suffix")
	}
}
