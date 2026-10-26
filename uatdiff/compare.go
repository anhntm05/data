package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	EnvOnlyProd = iota
	EnvOnlyUAT
	EnvBoth
)

var EnvLabel = map[int]string{
	EnvOnlyProd: "Chỉ có ở PROD",
	EnvOnlyUAT:  "Chỉ có ở UAT",
	EnvBoth:     "Có ở cả 2",
}

// ColumnCheck is the result of matching the headers of the two files.
type ColumnCheck struct {
	Common   []string // in UAT file order
	OnlyUAT  []string
	OnlyProd []string
}

func CheckColumns(uat, prod *Table) ColumnCheck {
	var c ColumnCheck
	inProd := map[string]bool{}
	for _, h := range prod.Headers {
		inProd[h] = true
	}
	inUAT := map[string]bool{}
	for _, h := range uat.Headers {
		inUAT[h] = true
		if inProd[h] {
			c.Common = append(c.Common, h)
		} else {
			c.OnlyUAT = append(c.OnlyUAT, h)
		}
	}
	for _, h := range prod.Headers {
		if !inUAT[h] {
			c.OnlyProd = append(c.OnlyProd, h)
		}
	}
	return c
}

func colIndex(t *Table) map[string]int {
	m := map[string]int{}
	for i, h := range t.Headers {
		m[h] = i
	}
	return m
}

func keyOf(row []string, idx []int, order DateOrder) string {
	parts := make([]string, len(idx))
	for i, c := range idx {
		parts[i] = CompareKey(row[c], order)
	}
	return strings.Join(parts, "\x1f")
}

// DupReport describes rows sharing the same key inside one file.
type DupReport struct {
	File      string
	DupKeys   int            // number of keys that appear more than once
	DupRows   int            // number of rows involved
	DiffCols  map[string]int // column -> number of duplicate keys where it differs
	Examples  []string       // a few duplicated keys, for display
}

// SuggestedCols returns the columns that tell duplicate rows apart,
// most useful first.
func (d DupReport) SuggestedCols() []string {
	var cols []string
	for c := range d.DiffCols {
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool {
		if d.DiffCols[cols[i]] != d.DiffCols[cols[j]] {
			return d.DiffCols[cols[i]] > d.DiffCols[cols[j]]
		}
		return cols[i] < cols[j]
	})
	return cols
}

func FindDuplicates(t *Table, keys, cols []string, order DateOrder) DupReport {
	ci := colIndex(t)
	kidx := make([]int, len(keys))
	for i, k := range keys {
		kidx[i] = ci[k]
	}
	groups := map[string][]int{}
	var orderKeys []string
	for i, r := range t.Rows {
		k := keyOf(r, kidx, order)
		if _, ok := groups[k]; !ok {
			orderKeys = append(orderKeys, k)
		}
		groups[k] = append(groups[k], i)
	}
	rep := DupReport{File: t.Name(), DiffCols: map[string]int{}}
	for _, k := range orderKeys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		rep.DupKeys++
		rep.DupRows += len(g)
		if len(rep.Examples) < 5 {
			rep.Examples = append(rep.Examples, strings.ReplaceAll(displayKey(t.Rows[g[0]], kidx, order), "\x1f", ", "))
		}
		for _, c := range cols {
			first := CompareKey(t.Rows[g[0]][ci[c]], order)
			for _, ri := range g[1:] {
				if CompareKey(t.Rows[ri][ci[c]], order) != first {
					rep.DiffCols[c]++
					break
				}
			}
		}
	}
	return rep
}

func displayKey(row []string, idx []int, order DateOrder) string {
	parts := make([]string, len(idx))
	for i, c := range idx {
		parts[i] = Display(row[c], order)
	}
	return strings.Join(parts, "\x1f")
}

// Options holds everything the user chose before running the comparison.
type Options struct {
	Keys     []string
	Compare  []string // non-key columns to compare, in output order
	Order    DateOrder
	Sort     []SortSpec
	EnvOrder [3]int // environment values in sort order
}

// SortSpec is one level of sorting. Field is "environment", "note",
// "khac_biet" or a column name.
type SortSpec struct {
	Field string
	Desc  bool
}

type ResultRow struct {
	Env      int
	Keys     []string
	UAT      []string // display values of Compare columns; nil if absent
	Prod     []string
	DiffCols []bool
	Diff     string
	Note     string
	NoteFrom bool // note came from the reference file
}

type Result struct {
	UATName, ProdName   string
	TotalUAT, TotalProd int
	OnlyUAT, OnlyProd   int
	Matched, MatchedDiff int
	NotesFilled         int
	Rows                []ResultRow
	Opts                Options
}

// Compare matches rows by key. Rows sharing a key within a file are paired
// in the order they appear.
func Compare(uat, prod *Table, o Options) *Result {
	res := &Result{UATName: uat.Name(), ProdName: prod.Name(),
		TotalUAT: len(uat.Rows), TotalProd: len(prod.Rows), Opts: o}
	ui, pi := colIndex(uat), colIndex(prod)
	ukeys, pkeys := make([]int, len(o.Keys)), make([]int, len(o.Keys))
	for i, k := range o.Keys {
		ukeys[i], pkeys[i] = ui[k], pi[k]
	}
	prodGroups := map[string][]int{}
	for i, r := range prod.Rows {
		k := keyOf(r, pkeys, o.Order)
		prodGroups[k] = append(prodGroups[k], i)
	}
	used := make([]bool, len(prod.Rows))
	pick := func(row []string, idx map[string]int) []string {
		out := make([]string, len(o.Compare))
		for i, c := range o.Compare {
			out[i] = Display(row[idx[c]], o.Order)
		}
		return out
	}
	keyVals := func(row []string, idx []int) []string {
		out := make([]string, len(idx))
		for i, c := range idx {
			out[i] = Display(row[c], o.Order)
		}
		return out
	}

	for _, ur := range uat.Rows {
		k := keyOf(ur, ukeys, o.Order)
		g := prodGroups[k]
		if len(g) == 0 {
			res.OnlyUAT++
			res.Rows = append(res.Rows, ResultRow{Env: EnvOnlyUAT, Keys: keyVals(ur, ukeys), UAT: pick(ur, ui)})
			continue
		}
		pr := prod.Rows[g[0]]
		used[g[0]] = true
		prodGroups[k] = g[1:]
		rr := ResultRow{Env: EnvBoth, Keys: keyVals(ur, ukeys), UAT: pick(ur, ui), Prod: pick(pr, pi),
			DiffCols: make([]bool, len(o.Compare))}
		var diffs []string
		for i, c := range o.Compare {
			if CompareKey(ur[ui[c]], o.Order) != CompareKey(pr[pi[c]], o.Order) {
				rr.DiffCols[i] = true
				diffs = append(diffs, fmt.Sprintf("%s: UAT=%s, PROD=%s", c, showEmpty(rr.UAT[i]), showEmpty(rr.Prod[i])))
			}
		}
		rr.Diff = strings.Join(diffs, "; ")
		res.Matched++
		if len(diffs) > 0 {
			res.MatchedDiff++
		}
		res.Rows = append(res.Rows, rr)
	}
	for i, pr := range prod.Rows {
		if !used[i] {
			res.OnlyProd++
			res.Rows = append(res.Rows, ResultRow{Env: EnvOnlyProd, Keys: keyVals(pr, pkeys), Prod: pick(pr, pi)})
		}
	}
	return res
}

func showEmpty(s string) string {
	if s == "" {
		return "(trống)"
	}
	return s
}

// NeedsNote is false for rows present on both sides with no difference.
func (r ResultRow) NeedsNote() bool { return r.Env != EnvBoth || r.Diff != "" }

// RefNotes maps a normalized key to the note found in a reference file.
type RefNotes struct {
	Notes  map[string]string
	Header int // 1-based row of the header in the reference file
	Rows   int
}

// LoadRefNotes finds the header row (the first row holding every key
// column and a "note" column, case-insensitive) and collects non-empty notes.
func LoadRefNotes(path string, keys []string, order DateOrder) (*RefNotes, error) {
	rows, err := ReadAnyRows(path)
	if err != nil {
		return nil, err
	}
	for hi, hdr := range rows {
		pos := map[string]int{}
		for i, h := range hdr {
			pos[strings.ToLower(strings.TrimSpace(unwrapExcelText(h)))] = i
		}
		noteCol, ok := pos["note"]
		if !ok {
			continue
		}
		kidx := make([]int, len(keys))
		found := true
		for i, k := range keys {
			c, ok := pos[strings.ToLower(k)]
			if !ok {
				found = false
				break
			}
			kidx[i] = c
		}
		if !found {
			continue
		}
		rn := &RefNotes{Notes: map[string]string{}, Header: hi + 1}
		for _, r := range rows[hi+1:] {
			get := func(i int) string {
				if i < len(r) {
					return unwrapExcelText(r[i])
				}
				return ""
			}
			note := strings.TrimSpace(get(noteCol))
			if note == "" {
				continue
			}
			parts := make([]string, len(kidx))
			for i, c := range kidx {
				parts[i] = CompareKey(get(c), order)
			}
			k := strings.Join(parts, "\x1f")
			if _, exists := rn.Notes[k]; !exists {
				rn.Notes[k] = note
				rn.Rows++
			}
		}
		return rn, nil
	}
	return nil, fmt.Errorf("không tìm thấy dòng header có đủ các cột key (%s) và cột note", strings.Join(keys, ", "))
}

// ApplyNotes copies notes from the reference into rows that need one.
func (res *Result) ApplyNotes(rn *RefNotes) {
	for i := range res.Rows {
		r := &res.Rows[i]
		if !r.NeedsNote() {
			continue
		}
		parts := make([]string, len(r.Keys))
		for j, v := range r.Keys {
			parts[j] = CompareKey(v, res.Opts.Order)
		}
		if n, ok := rn.Notes[strings.Join(parts, "\x1f")]; ok {
			r.Note, r.NoteFrom = n, true
			res.NotesFilled++
		}
	}
}

// SortRows applies the sort levels. Empty values always go last.
func (res *Result) SortRows() {
	o := res.Opts
	envRank := map[int]int{}
	for i, e := range o.EnvOrder {
		envRank[e] = i
	}
	keyPos, cmpPos := map[string]int{}, map[string]int{}
	for i, k := range o.Keys {
		keyPos[k] = i
	}
	for i, c := range o.Compare {
		cmpPos[c] = i
	}
	value := func(r *ResultRow, f string) string {
		switch f {
		case "note":
			return r.Note
		case "khac_biet":
			return r.Diff
		}
		if i, ok := keyPos[f]; ok {
			return r.Keys[i]
		}
		if i, ok := cmpPos[f]; ok {
			if r.UAT != nil {
				return r.UAT[i]
			}
			return r.Prod[i]
		}
		return ""
	}
	sort.SliceStable(res.Rows, func(a, b int) bool {
		ra, rb := &res.Rows[a], &res.Rows[b]
		for _, s := range o.Sort {
			if s.Field == "environment" {
				if d := envRank[ra.Env] - envRank[rb.Env]; d != 0 {
					return d < 0
				}
				continue
			}
			va, vb := value(ra, s.Field), value(rb, s.Field)
			if va == vb {
				continue
			}
			if va == "" || vb == "" {
				return vb == ""
			}
			c := lessValue(va, vb)
			if c == 0 {
				continue
			}
			if s.Desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
}
