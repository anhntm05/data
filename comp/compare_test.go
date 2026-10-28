package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
