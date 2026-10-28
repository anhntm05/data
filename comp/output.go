package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const (
	colEnv  = "environment"
	colDiff = "Khac biet"
	colNote = "note"
)

func (res *Result) Header() []string {
	h := append([]string{}, res.Opts.Keys...)
	h = append(h, colEnv)
	for _, c := range res.Opts.Compare {
		h = append(h, c+"_uat", c+"_prod")
	}
	return append(h, colDiff, colNote)
}

func (res *Result) SummaryLines() []string {
	return []string{
		fmt.Sprintf("So sánh file %s (UAT) với file %s (PROD)", res.UATName, res.ProdName),
		fmt.Sprintf("Tổng file %s: %d | Tổng file %s: %d", res.UATName, res.TotalUAT, res.ProdName, res.TotalProd),
		fmt.Sprintf("Chỉ có ở UAT: %d | Chỉ có ở PROD: %d", res.OnlyUAT, res.OnlyProd),
		fmt.Sprintf("Các dòng trùng theo key: %d (khác dữ liệu: %d | giống hoàn toàn: %d)",
			res.Matched, res.MatchedDiff, res.Matched-res.MatchedDiff),
		fmt.Sprintf("Key: %s | Note lấy từ file reference: %d | Tạo lúc: %s",
			strings.Join(res.Opts.Keys, ", "), res.NotesFilled, time.Now().Format("2006-01-02 15:04:05")),
	}
}

func (res *Result) rowValues(r ResultRow) []string {
	v := append([]string{}, r.Keys...)
	v = append(v, EnvLabel[r.Env])
	for i := range res.Opts.Compare {
		var u, p string
		if r.UAT != nil {
			u = r.UAT[i]
		}
		if r.Prod != nil {
			p = r.Prod[i]
		}
		v = append(v, u, p)
	}
	return append(v, r.Diff, r.Note)
}

// WriteCSV writes UTF-8 with BOM so Excel shows Vietnamese correctly.
// Values that Excel would turn into numbers or dates are written as
// ="value" so they open as text, exactly as in the source.
func (res *Result) WriteCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	f.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(f)
	for _, l := range res.SummaryLines() {
		w.Write([]string{l})
	}
	w.Write([]string{})
	w.Write(res.Header())
	for _, r := range res.Rows {
		vals := res.rowValues(r)
		for i, v := range vals {
			vals[i] = excelText(v)
		}
		w.Write(vals)
	}
	w.Flush()
	return w.Error()
}

func excelText(v string) string {
	if v == "" || len(v) > 250 || strings.ContainsAny(v, "\r\n") {
		return v
	}
	hasDigit := strings.IndexFunc(v, unicode.IsDigit) >= 0
	if !hasDigit && !strings.ContainsAny(v, "=+-@") {
		return v
	}
	return `="` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

type xlsxStyles struct {
	f     *excelize.File
	cache map[string]int
}

func (s *xlsxStyles) get(name string, st *excelize.Style) int {
	if id, ok := s.cache[name]; ok {
		return id
	}
	id, err := s.f.NewStyle(st)
	if err != nil {
		panic(err)
	}
	s.cache[name] = id
	return id
}

var thin = []excelize.Border{
	{Type: "left", Color: "BFBFBF", Style: 1}, {Type: "right", Color: "BFBFBF", Style: 1},
	{Type: "top", Color: "BFBFBF", Style: 1}, {Type: "bottom", Color: "BFBFBF", Style: 1},
}

func fill(c string) excelize.Fill {
	if c == "" {
		return excelize.Fill{}
	}
	return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{c}}
}

var envFill = map[int]string{EnvOnlyProd: "FCE4D6", EnvOnlyUAT: "DDEBF7", EnvBoth: ""}

func (s *xlsxStyles) header(bg string) int {
	return s.get("h"+bg, &excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      fill(bg),
		Border:    thin,
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
}

// cell kinds: "" normal, "diff" differing value, "kb" Khac biet text,
// "note" note from reference, "env" environment label.
func (s *xlsxStyles) cell(env int, kind string) int {
	st := &excelize.Style{NumFmt: 49, Border: thin, Fill: fill(envFill[env]),
		Alignment: &excelize.Alignment{Vertical: "top"}}
	switch kind {
	case "diff":
		st.Fill = fill("FFC7CE")
		st.Font = &excelize.Font{Color: "9C0006", Bold: true}
	case "kb":
		st.Font = &excelize.Font{Color: "9C0006"}
	case "note":
		st.Fill = fill("E2EFDA")
		st.Font = &excelize.Font{Color: "375623"}
	case "env":
		st.Font = &excelize.Font{Bold: true}
	}
	return s.get(fmt.Sprintf("c%d%s", env, kind), st)
}

func (res *Result) WriteXLSX(path string) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Compare"
	f.SetSheetName("Sheet1", sheet)
	st := &xlsxStyles{f: f, cache: map[string]int{}}
	header := res.Header()
	nk, nc := len(res.Opts.Keys), len(res.Opts.Compare)
	last := len(header)

	title := st.get("title", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 14, Color: "1F4E78"}})
	sum := st.get("sum", &excelize.Style{Font: &excelize.Font{Bold: true, Color: "404040"}})
	for i, l := range res.SummaryLines() {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		f.SetCellStr(sheet, cell, l)
		id := sum
		if i == 0 {
			id = title
		}
		f.SetCellStyle(sheet, cell, cell, id)
	}
	hr := len(res.SummaryLines()) + 2 // one blank line, then header

	hcell := func(c int) string { n, _ := excelize.CoordinatesToCellName(c, hr); return n }
	hdr := make([]interface{}, len(header))
	for i, h := range header {
		hdr[i] = h
	}
	f.SetSheetRow(sheet, hcell(1), &hdr)
	f.SetCellStyle(sheet, hcell(1), hcell(nk), st.header("1F4E78"))
	f.SetCellStyle(sheet, hcell(nk+1), hcell(nk+1), st.header("7030A0"))
	for i := 0; i < nc; i++ {
		f.SetCellStyle(sheet, hcell(nk+2+2*i), hcell(nk+2+2*i), st.header("2F75B5"))
		f.SetCellStyle(sheet, hcell(nk+3+2*i), hcell(nk+3+2*i), st.header("C65911"))
	}
	f.SetCellStyle(sheet, hcell(last-1), hcell(last-1), st.header("C00000"))
	f.SetCellStyle(sheet, hcell(last), hcell(last), st.header("548235"))

	width := make([]int, last)
	for i, h := range header {
		width[i] = utf8.RuneCountInString(h)
	}
	for ri, r := range res.Rows {
		row := hr + 1 + ri
		cn := func(c int) string { n, _ := excelize.CoordinatesToCellName(c, row); return n }
		vals := res.rowValues(r)
		rv := make([]interface{}, len(vals))
		for i, v := range vals {
			rv[i] = v
			if n := utf8.RuneCountInString(v); n > width[i] {
				width[i] = n
			}
		}
		f.SetSheetRow(sheet, cn(1), &rv)
		f.SetCellStyle(sheet, cn(1), cn(last), st.cell(r.Env, ""))
		f.SetCellStyle(sheet, cn(nk+1), cn(nk+1), st.cell(r.Env, "env"))
		for i, d := range r.DiffCols {
			if d {
				f.SetCellStyle(sheet, cn(nk+2+2*i), cn(nk+3+2*i), st.cell(r.Env, "diff"))
			}
		}
		if r.Diff != "" {
			f.SetCellStyle(sheet, cn(last-1), cn(last-1), st.cell(r.Env, "kb"))
		}
		if r.NoteFrom {
			f.SetCellStyle(sheet, cn(last), cn(last), st.cell(r.Env, "note"))
		}
	}
	for i, w := range width {
		max := 45
		if i == last-2 {
			max = 80
		}
		ww := float64(w) + 2
		if ww < 10 {
			ww = 10
		}
		if ww > float64(max) {
			ww = float64(max)
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, col, col, ww)
	}
	f.SetRowHeight(sheet, hr, 30)

	lastCell, _ := excelize.CoordinatesToCellName(last, hr+len(res.Rows))
	if err := f.AutoFilter(sheet, hcell(1)+":"+lastCell, nil); err != nil {
		return err
	}
	topLeft, _ := excelize.CoordinatesToCellName(nk+2, hr+1)
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, XSplit: nk + 1, YSplit: hr,
		TopLeftCell: topLeft, ActivePane: "bottomRight"}); err != nil {
		return err
	}
	return f.SaveAs(path)
}
