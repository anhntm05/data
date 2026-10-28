package main

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const (
	sheetSummary = "Tổng hợp"
	sheetRecords = "records"
	sheetFields  = "field_mismatch"
	colStatus    = "trạng thái"
	colField     = "tên trường"
	colValue1    = "giá trị file 1"
	colValue2    = "giá trị file 2"
)

// table is one output tab: its header, its rows, and the kind of each row
// ("only1", "only2", "match" or "diff") for coloring.
type table struct {
	header []string
	rows   [][]string
	kind   []string
}

// recordsTable builds the records tab: keys, status, common columns, then
// file 1 only columns (_file1) and file 2 only columns (_file2), then note.
func (res *Result) recordsTable(note *NoteMap) table {
	t := table{}
	t.header = append(t.header, res.Opts.Keys...)
	t.header = append(t.header, colStatus)
	t.header = append(t.header, res.Common...)
	for _, c := range res.Check.OnlyUAT {
		t.header = append(t.header, c+"_file1")
	}
	for _, c := range res.Check.OnlyProd {
		t.header = append(t.header, c+"_file2")
	}
	if note != nil {
		t.header = append(t.header, note.Col)
	}
	for _, r := range res.Rows {
		if r.Status == StatusDiff {
			continue
		}
		vals := make([]string, 0, len(t.header))
		vals = append(vals, r.Keys...)
		vals = append(vals, StatusLabel[r.Status])
		vals = append(vals, padTo(r.Common, len(res.Common))...)
		vals = append(vals, padTo(r.Only1, len(res.Check.OnlyUAT))...)
		vals = append(vals, padTo(r.Only2, len(res.Check.OnlyProd))...)
		if note != nil {
			n := ""
			if r.Status != StatusMatch {
				n, _ = note.Lookup(r.Keys, res.Opts.Order)
			}
			vals = append(vals, n)
		}
		t.rows = append(t.rows, vals)
		t.kind = append(t.kind, kindOf(r.Status))
	}
	return t
}

// fieldsTable builds the field_mismatch tab: one line per differing column,
// lines of the same key next to each other, sorted by field name.
func (res *Result) fieldsTable(note *NoteMap) table {
	t := table{}
	t.header = append(t.header, res.Opts.Keys...)
	t.header = append(t.header, colField, colValue1, colValue2)
	if note != nil {
		t.header = append(t.header, note.Col)
	}
	for _, r := range res.Rows {
		if r.Status != StatusDiff {
			continue
		}
		diffs := append([]FieldDiff{}, r.Diffs...)
		sort.SliceStable(diffs, func(a, b int) bool { return diffs[a].Field < diffs[b].Field })
		for _, d := range diffs {
			vals := make([]string, 0, len(t.header))
			vals = append(vals, r.Keys...)
			vals = append(vals, d.Field, showEmpty(d.V1), showEmpty(d.V2))
			if note != nil {
				// The note map for this tab matches on the keys plus tên trường.
				n, _ := note.Lookup(append(append([]string{}, r.Keys...), d.Field), res.Opts.Order)
				vals = append(vals, n)
			}
			t.rows = append(t.rows, vals)
			t.kind = append(t.kind, "diff")
		}
	}
	return t
}

func kindOf(status int) string {
	switch status {
	case StatusOnly1:
		return "only1"
	case StatusOnly2:
		return "only2"
	case StatusMatch:
		return "match"
	}
	return "diff"
}

func padTo(vals []string, n int) []string {
	out := make([]string, n)
	copy(out, vals)
	return out
}

func (res *Result) summaryRows() [][]string {
	return [][]string{
		{"Tên file 1", res.File1},
		{"Tên file 2", res.File2},
		{"Tổng row file 1", fmt.Sprint(res.Total1)},
		{"Tổng row file 2", fmt.Sprint(res.Total2)},
		{"Chỉ có ở file 1", fmt.Sprint(res.Only1)},
		{"Chỉ có ở file 2", fmt.Sprint(res.Only2)},
		{"Trùng khớp hoàn toàn", fmt.Sprint(res.Matched - res.MatchedDiff)},
		{"Khác nhau 1 cột", fmt.Sprint(res.DiffOne)},
		{"Khác nhau nhiều cột", fmt.Sprint(res.DiffMany)},
	}
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

var kindFill = map[string]string{"only1": "DDEBF7", "only2": "FCE4D6", "match": "", "diff": ""}

func (s *xlsxStyles) header(bg string) int {
	return s.get("h"+bg, &excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      fill(bg),
		Border:    thin,
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
}

// cell returns a style for a data cell. kind is the row kind; role is
// "" (plain), "diff" (differing value), "note" or "field" (field name).
func (s *xlsxStyles) cell(kind, role string) int {
	st := &excelize.Style{Border: thin, Fill: fill(kindFill[kind]),
		Alignment: &excelize.Alignment{Vertical: "top"}}
	switch role {
	case "diff":
		st.Fill = fill("FFC7CE")
		st.Font = &excelize.Font{Color: "9C0006", Bold: true}
	case "note":
		st.Fill = fill("E2EFDA")
		st.Font = &excelize.Font{Color: "375623"}
	case "field":
		st.Font = &excelize.Font{Bold: true}
	}
	return s.get("c"+kind+role, st)
}

// writeTable writes a header and rows, styles them, and sets filter and
// freeze panes. Values are written as text so Excel never converts them.
func writeTable(f *excelize.File, st *xlsxStyles, sheet string, t table, nk, noteCol int, roleOf func(ci int) string) error {
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	cellName := func(c, r int) string { n, _ := excelize.CoordinatesToCellName(c, r); return n }
	last := len(t.header)
	hdr := make([]interface{}, last)
	width := make([]int, last)
	for i, h := range t.header {
		hdr[i] = h
		width[i] = utf8.RuneCountInString(h)
	}
	f.SetSheetRow(sheet, cellName(1, 1), &hdr)
	for c := 1; c <= last; c++ {
		bg := "2F75B5" // data columns
		switch {
		case c <= nk:
			bg = "1F4E78" // key columns
		case t.header[c-1] == colStatus:
			bg = "7030A0"
		case c == noteCol:
			bg = "548235"
		}
		f.SetCellStyle(sheet, cellName(c, 1), cellName(c, 1), st.header(bg))
	}
	for ri, row := range t.rows {
		r := ri + 2
		for ci, v := range row {
			if v != "" {
				f.SetCellStr(sheet, cellName(ci+1, r), v)
			}
			if n := utf8.RuneCountInString(v); n > width[ci] {
				width[ci] = n
			}
			role := ""
			if roleOf != nil {
				role = roleOf(ci)
			}
			if ci == noteCol-1 {
				role = "note"
			}
			f.SetCellStyle(sheet, cellName(ci+1, r), cellName(ci+1, r), st.cell(t.kind[ri], role))
		}
	}
	for i, w := range width {
		col, _ := excelize.ColumnNumberToName(i + 1)
		ww := float64(w) + 2
		if ww < 10 {
			ww = 10
		}
		if ww > 45 {
			ww = 45
		}
		f.SetColWidth(sheet, col, col, ww)
	}
	f.SetRowHeight(sheet, 1, 30)
	if len(t.rows) > 0 {
		if err := f.AutoFilter(sheet, cellName(1, 1)+":"+cellName(last, len(t.rows)+1), nil); err != nil {
			return err
		}
	}
	topLeft := cellName(nk+1, 2)
	return f.SetPanes(sheet, &excelize.Panes{Freeze: true, XSplit: nk, YSplit: 1,
		TopLeftCell: topLeft, ActivePane: "bottomRight"})
}

// WriteXLSX writes the three tabs. notes maps a tab name to the reference
// notes for that tab; a missing entry means no notes on that tab.
func (res *Result) WriteXLSX(path string, notes map[string]*NoteMap) error {
	f := excelize.NewFile()
	defer f.Close()
	st := &xlsxStyles{f: f, cache: map[string]int{}}
	f.SetSheetName("Sheet1", sheetSummary)

	title := st.get("title", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12, Color: "1F4E78"}})
	f.SetCellStr(sheetSummary, "A1", "Thông tin")
	f.SetCellStr(sheetSummary, "B1", "Giá trị")
	f.SetCellStyle(sheetSummary, "A1", "B1", st.header("1F4E78"))
	for i, kv := range res.summaryRows() {
		r := i + 2
		a, _ := excelize.CoordinatesToCellName(1, r)
		b, _ := excelize.CoordinatesToCellName(2, r)
		f.SetCellStr(sheetSummary, a, kv[0])
		f.SetCellStr(sheetSummary, b, kv[1])
		f.SetCellStyle(sheetSummary, a, a, title)
		f.SetCellStyle(sheetSummary, b, b, st.cell("", ""))
	}
	f.SetColWidth(sheetSummary, "A", "A", 30)
	f.SetColWidth(sheetSummary, "B", "B", 40)

	nk := len(res.Opts.Keys)
	rn := notes[sheetRecords]
	recs := res.recordsTable(rn)
	noteCol := 0
	if rn != nil {
		noteCol = len(recs.header)
	}
	if err := writeTable(f, st, sheetRecords, recs, nk, noteCol, nil); err != nil {
		return err
	}
	fn := notes[sheetFields]
	fields := res.fieldsTable(fn)
	noteCol = 0
	if fn != nil {
		noteCol = len(fields.header)
	}
	valueCol := nk + 2 // 1-based column of "giá trị file 1"
	roleOf := func(ci int) string {
		if ci+1 == valueCol || ci+1 == valueCol+1 {
			return "diff"
		}
		if ci == nk {
			return "field"
		}
		return ""
	}
	if err := writeTable(f, st, sheetFields, fields, nk, noteCol, roleOf); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	return f.SaveAs(path)
}
