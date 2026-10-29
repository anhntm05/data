package main

import (
	"sort"
	"strings"
)

// Row status. "file 1" is the first file picked (UAT), "file 2" the second (PROD).
const (
	StatusOnly1 = iota // key only in file 1
	StatusOnly2        // key only in file 2
	StatusMatch        // key in both files, every compared column equal
	StatusDiff         // key in both files, at least one compared column differs
)

// StatusLabel is the text shown in the trạng thái column.
var StatusLabel = map[int]string{
	StatusOnly1: "chỉ file 1",
	StatusOnly2: "chỉ file 2",
	StatusMatch: "khớp hoàn toàn",
	StatusDiff:  "khác",
}

// ColumnCheck is the result of matching the headers of the two files.
type ColumnCheck struct {
	Common   []string // in file 1 order
	OnlyUAT  []string // only in file 1
	OnlyProd []string // only in file 2
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
	File     string
	DupKeys  int            // number of keys that appear more than once
	DupRows  int            // number of rows involved
	DiffCols map[string]int // column -> number of duplicate keys where it differs
	Examples []string       // a few duplicated keys, for display
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
	Keys    []string
	Compare []string // non-key columns to compare, in output order
	Order   DateOrder
}

// FieldDiff is one differing column of a matched key.
type FieldDiff struct {
	Field string
	V1    string // display value in file 1
	V2    string // display value in file 2
}

// ResultRow is one key. Value slices follow the column lists in Result:
// Common for Result.Common, Only1 for Check.OnlyUAT, Only2 for Check.OnlyProd.
// A nil slice means the row does not exist in that file.
type ResultRow struct {
	Status int
	Keys   []string
	Common []string
	Only1  []string
	Only2  []string
	Diffs  []FieldDiff
}

type Result struct {
	File1, File2         string
	Total1, Total2       int
	Only1, Only2         int
	Matched, MatchedDiff int // keys present in both files; MatchedDiff = at least one diff
	DiffOne, DiffMany    int // MatchedDiff split by number of differing columns
	Check                ColumnCheck
	Common               []string // Check.Common without the key columns
	Rows                 []ResultRow
	Opts                 Options
}

// Compare matches rows by key. Rows sharing a key within a file are paired
// in the order they appear. Rows are sorted by status, then by key.
func Compare(uat, prod *Table, o Options) *Result {
	cc := CheckColumns(uat, prod)
	res := &Result{File1: uat.Name(), File2: prod.Name(),
		Total1: len(uat.Rows), Total2: len(prod.Rows), Check: cc, Opts: o,
		Common: without(cc.Common, o.Keys)}
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
	pick := func(row []string, idx map[string]int, cols []string) []string {
		out := make([]string, len(cols))
		for i, c := range cols {
			out[i] = Display(row[idx[c]], o.Order)
		}
		return out
	}

	for _, ur := range uat.Rows {
		k := keyOf(ur, ukeys, o.Order)
		g := prodGroups[k]
		if len(g) == 0 {
			res.Only1++
			res.Rows = append(res.Rows, ResultRow{Status: StatusOnly1,
				Keys: pick(ur, ui, o.Keys), Common: pick(ur, ui, res.Common), Only1: pick(ur, ui, cc.OnlyUAT)})
			continue
		}
		pr := prod.Rows[g[0]]
		used[g[0]] = true
		prodGroups[k] = g[1:]
		rr := ResultRow{Keys: pick(ur, ui, o.Keys), Common: pick(ur, ui, res.Common),
			Only1: pick(ur, ui, cc.OnlyUAT), Only2: pick(pr, pi, cc.OnlyProd)}
		for _, c := range o.Compare {
			if CompareKey(ur[ui[c]], o.Order) != CompareKey(pr[pi[c]], o.Order) {
				rr.Diffs = append(rr.Diffs, FieldDiff{Field: c,
					V1: Display(ur[ui[c]], o.Order), V2: Display(pr[pi[c]], o.Order)})
			}
		}
		res.Matched++
		rr.Status = StatusMatch
		if n := len(rr.Diffs); n > 0 {
			rr.Status = StatusDiff
			res.MatchedDiff++
			if n == 1 {
				res.DiffOne++
			} else {
				res.DiffMany++
			}
		}
		res.Rows = append(res.Rows, rr)
	}
	for i, pr := range prod.Rows {
		if !used[i] {
			res.Only2++
			res.Rows = append(res.Rows, ResultRow{Status: StatusOnly2,
				Keys: pick(pr, pi, o.Keys), Common: pick(pr, pi, res.Common), Only2: pick(pr, pi, cc.OnlyProd)})
		}
	}
	sortRows(res.Rows)
	return res
}

// sortRows orders by status (file 1 only, file 2 only, match, diff), then
// by the key columns. Rows with the same key stay next to each other.
func sortRows(rows []ResultRow) {
	sort.SliceStable(rows, func(a, b int) bool {
		ra, rb := rows[a], rows[b]
		if ra.Status != rb.Status {
			return ra.Status < rb.Status
		}
		for i := range ra.Keys {
			if c := lessValue(ra.Keys[i], rb.Keys[i]); c != 0 {
				return c < 0
			}
		}
		return false
	})
}

func showEmpty(s string) string {
	if s == "" {
		return "(trống)"
	}
	return s
}
