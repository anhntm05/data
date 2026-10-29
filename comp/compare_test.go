package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/xuri/excelize/v2"
)

func TestNormalize(t *testing.T) {
	eq := [][2]string{
		{"1.0", "1"}, {" 1 ", "1"}, {"NULL", ""}, {"null", "  "},
		{"2026-10-26", "26/10/2026"}, {"2026-10-26 00:00:00", "2026-10-26"},
		{"2026/10/26 13:05:09", "26/10/2026 1:05:09 PM"}, {"26-OCT-26", "2026-10-26"},
		{"-0.0", "0"}, {"12435678654.040010", "12435678654.04001"},
	}
	for _, p := range eq {
		if CompareKey(p[0], DMY) != CompareKey(p[1], DMY) {
			t.Errorf("%q should equal %q", p[0], p[1])
		}
	}
	ne := [][2]string{
		{"HOA", "hoa"}, {"12435678654.04001", "12435678654.03001"}, {"007", "7"},
		{"03/04/2026", "2026-03-04"},
	}
	for _, p := range ne {
		if CompareKey(p[0], DMY) == CompareKey(p[1], DMY) {
			t.Errorf("%q should differ from %q", p[0], p[1])
		}
	}
	if Display("03/04/2026", MDY) != "2026-03-04" || Display("03/04/2026", DMY) != "2026-04-03" {
		t.Error("date order")
	}
	if Display("2026-10-26T08:30:00.120", DMY) != "2026-10-26 08:30:00.12" {
		t.Error(Display("2026-10-26T08:30:00.120", DMY))
	}
}

func write(t *testing.T, dir, name, body string) string {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompareFlow(t *testing.T) {
	dir := t.TempDir()
	u := write(t, dir, "uat.csv", "\xef\xbb\xbfid,branch,amount,status,ngay,only_u\n"+
		"1,HN,100.0,OK,2026-10-01,x\n2,HN,200,Lỗi,2026-10-02,x\n3,HCM,300,OK,,x\n4,HN,1,A,,x\n4,HN,2,B,,x\n")
	p := write(t, dir, "prod.csv", "id,branch,amount,status,ngay,only_p\n"+
		"1,HN,100,OK,01/10/2026,y\n2,HN,250,lỗi,2026-10-02,y\n5,DN,1,OK,NULL,y\n4,HN,1,A,,y\n")
	uat, err := ReadCSV(u)
	if err != nil {
		t.Fatal(err)
	}
	prod, _ := ReadCSV(p)
	cc := CheckColumns(uat, prod)
	if strings.Join(cc.OnlyUAT, ",") != "only_u" || strings.Join(cc.OnlyProd, ",") != "only_p" {
		t.Fatal(cc)
	}
	keys := []string{"id", "branch"}
	nonKeys := without(cc.Common, keys)
	d := FindDuplicates(uat, keys, nonKeys, DMY)
	if d.DupKeys != 1 || d.DiffCols["amount"] != 1 || d.DiffCols["status"] != 1 {
		t.Fatalf("%+v", d)
	}
	res := Compare(uat, prod, Options{Keys: keys, Compare: nonKeys, Order: DMY})
	if res.Only1 != 2 || res.Only2 != 1 || res.Matched != 3 || res.MatchedDiff != 1 || res.DiffMany != 1 {
		t.Fatalf("%+v", res)
	}
	var statuses []int
	for _, r := range res.Rows {
		statuses = append(statuses, r.Status)
		if r.Status == StatusDiff && (len(r.Diffs) != 2 || r.Diffs[0].V2 != "250") {
			t.Errorf("%+v", r.Diffs)
		}
	}
	if statuses[0] != StatusOnly1 || statuses[len(statuses)-1] != StatusDiff {
		t.Error(statuses)
	}

	// reference file in the CSV output layout, with ="..." wrapped keys
	ref := write(t, dir, "ref.csv", "So sanh\n\nid,branch,environment,note\n"+
		"=\"2\",HN,x,data mask\n=\"1\",HN,x,should not apply\n=\"5\",DN,x,fake test AA-1\n")
	rows, err := ReadSheetRows(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	hi, _, err := FindHeader(rows, keys)
	if err != nil {
		t.Fatal(err)
	}
	nm, err := BuildNoteMap(rows, hi, "note", "note", keys, DMY)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := nm.Lookup([]string{"2", "HN"}, DMY); !ok || n != "data mask" {
		t.Error(n, ok)
	}
	if _, ok := nm.Lookup([]string{"3", "HCM"}, DMY); ok {
		t.Error("unexpected note")
	}

	out := filepath.Join(dir, "out.xlsx")
	if err := res.WriteXLSX(out, map[string]*NoteMap{sheetRecords: nm, sheetFields: nm}); err != nil {
		t.Fatal(err)
	}
	names, err := SheetNames(out)
	if err != nil || strings.Join(names, "|") != "Tổng hợp|records|field_mismatch" {
		t.Fatal(err, names)
	}
	recs, err := ReadSheetRows(out, sheetRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 6 { // header + 5 rows (diff rows are not in records)
		t.Fatalf("records rows %d", len(recs))
	}
	fields, _ := ReadSheetRows(out, sheetFields)
	if len(fields) != 3 { // header + 2 field lines
		t.Fatalf("field rows %d", len(fields))
	}
}

func TestRefNotesSubsetOfKeys(t *testing.T) {
	dir := t.TempDir()
	ref := write(t, dir, "ref.csv", "title\nid,environment,note\n1,x,mask\n")
	rows, err := ReadSheetRows(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	hi, _, err := FindHeader(rows, []string{"id", "branch"})
	if err != nil {
		t.Fatal(err)
	}
	nm, err := BuildNoteMap(rows, hi, "note", "note", []string{"id", "branch"}, DMY)
	if err != nil {
		t.Fatal(err)
	}
	if len(nm.Missing) != 1 || nm.Missing[0] != "branch" || len(nm.KeyPos) != 1 {
		t.Fatalf("%+v", nm)
	}
	if n, ok := nm.Lookup([]string{"1", "HN"}, DMY); !ok || n != "mask" {
		t.Fatalf("%q %v", n, ok)
	}
}

func TestReadXLSXInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.xlsx")
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Info")
	f.NewSheet("Data")
	f.SetSheetRow("Data", "A1", &[]interface{}{"id", "ngay", "amount", "code"})
	f.SetSheetRow("Data", "A2", &[]interface{}{1, time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), 69274598800.73578, "007"})
	f.SetSheetRow("Data", "A3", &[]interface{}{2, time.Date(2026, 10, 27, 8, 30, 0, 0, time.UTC), 1.5, "A"})
	dateStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 14})
	f.SetCellStyle("Data", "B2", "B2", dateStyle)
	custom := "dd/mm/yyyy hh:mm"
	timeStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &custom})
	f.SetCellStyle("Data", "B3", "B3", timeStyle)
	money, _ := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	f.SetCellStyle("Data", "C2", "C3", money)
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	f.Close()

	names, err := SheetNames(path)
	if err != nil || strings.Join(names, ",") != "Info,Data" {
		t.Fatal(err, names)
	}
	tb, err := LoadTable(path, "Data")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"1", "2026-10-26", "69274598800.73578", "007"}, {"2", "2026-10-27 08:30:00", "1.5", "A"}}
	for i, w := range want {
		if strings.Join(tb.Rows[i], "|") != strings.Join(w, "|") {
			t.Errorf("row %d = %v, want %v", i, tb.Rows[i], w)
		}
	}
}

func TestFieldNotesMatchFieldName(t *testing.T) {
	dir := t.TempDir()
	ref := write(t, dir, "ref.csv", "gd_id,tên trường,giá trị file 1,giá trị file 2,IT Feedback\n"+
		"30,amount,1,2,dữ liệu sửa\n30,cust_name,A,***MASKED***,dữ liệu mask\n")
	rows, _ := ReadSheetRows(ref, "")
	match := []string{"gd_id", colField}
	hi, _, err := FindHeader(rows, match)
	if err != nil {
		t.Fatal(err)
	}
	nm, err := BuildNoteMap(rows, hi, "IT Feedback", "IT Feedback", match, DMY)
	if err != nil || len(nm.Missing) != 0 {
		t.Fatal(err, nm)
	}
	res := &Result{Opts: Options{Keys: []string{"gd_id"}}, Rows: []ResultRow{{Status: StatusDiff, Keys: []string{"30"},
		Diffs: []FieldDiff{{Field: "cust_name", V1: "A", V2: "***MASKED***"}, {Field: "amount", V1: "1", V2: "2"}}}}}
	tb := res.fieldsTable(nm)
	got := map[string]string{}
	for _, r := range tb.rows {
		got[r[1]] = r[len(r)-1]
	}
	if got["amount"] != "dữ liệu sửa" || got["cust_name"] != "dữ liệu mask" {
		t.Fatal(got)
	}
}

func TestBackFormEsc(t *testing.T) {
	var picked []string
	newForm := func() *backForm {
		f := huh.NewForm(huh.NewGroup(huh.NewMultiSelect[string]().
			Options(huh.NewOptions("a", "b")...).Value(&picked).Filterable(true)))
		f.Init()
		return &backForm{form: f}
	}
	m := newForm()
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.back {
		t.Fatal("Esc should go back")
	}
	m = newForm()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.back {
		t.Fatal("Esc while filtering should only clear the filter")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.back {
		t.Fatal("second Esc should go back")
	}
}
