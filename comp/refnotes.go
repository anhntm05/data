package main

import (
	"fmt"
	"strings"
)

// NoteMap holds notes from one sheet of the reference file, looked up by key.
type NoteMap struct {
	Col     string // output column that receives the note
	src     string // reference column the notes come from
	KeyPos  []int  // positions in Options.Keys that the reference also has
	Missing []string
	Values  map[string]string
	Apply   map[int]bool // row statuses (Status*) that take notes from this map; nil means all
}

// applies reports whether rows with this status take notes from nm.
func (nm *NoteMap) applies(status int) bool {
	return nm.Apply == nil || nm.Apply[status]
}

// lookupAny returns the note for a row from the first map that applies to
// its status and has a note for the key.
func lookupAny(nms []*NoteMap, status int, keys []string, order DateOrder) string {
	for _, nm := range nms {
		if !nm.applies(status) {
			continue
		}
		if n, ok := nm.Lookup(keys, order); ok {
			return n
		}
	}
	return ""
}

// FindHeader returns the row (0-based) that holds the most key columns,
// and that row's cells. Ties go to the first row.
func FindHeader(rows [][]string, keys []string) (int, []string, error) {
	best, bestN := -1, 0
	for hi, r := range rows {
		n := 0
		have := map[string]bool{}
		for _, h := range r {
			have[strings.ToLower(strings.TrimSpace(unwrapExcelText(h)))] = true
		}
		for _, k := range keys {
			if have[strings.ToLower(k)] {
				n++
			}
		}
		if n > bestN {
			best, bestN = hi, n
		}
	}
	if best < 0 {
		return 0, nil, fmt.Errorf("không tìm thấy dòng header có cột key (%s)", strings.Join(keys, ", "))
	}
	hdr := make([]string, len(rows[best]))
	for i, h := range rows[best] {
		hdr[i] = unwrapExcelText(h)
	}
	return best, hdr, nil
}

// BuildNoteMap reads notes from column source (a header of rows[hi]) and
// matches them to the key columns the reference has by name.
func BuildNoteMap(rows [][]string, hi int, source, target string, keys []string, order DateOrder) (*NoteMap, error) {
	pos := map[string]int{}
	for i, h := range rows[hi] {
		pos[strings.ToLower(strings.TrimSpace(unwrapExcelText(h)))] = i
	}
	noteCol, ok := pos[strings.ToLower(strings.TrimSpace(source))]
	if !ok {
		return nil, fmt.Errorf("không tìm thấy cột %q trong file reference", source)
	}
	nm := &NoteMap{Col: target, Values: map[string]string{}}
	var cols []int
	for i, k := range keys {
		if c, ok := pos[strings.ToLower(k)]; ok {
			cols = append(cols, c)
			nm.KeyPos = append(nm.KeyPos, i)
		} else {
			nm.Missing = append(nm.Missing, k)
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("file reference không có cột key nào (%s)", strings.Join(keys, ", "))
	}
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
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = CompareKey(get(c), order)
		}
		k := strings.Join(parts, "\x1f")
		if _, exists := nm.Values[k]; !exists {
			nm.Values[k] = note
		}
	}
	return nm, nil
}

// Lookup returns the note for a row's key values, if the reference has one.
func (nm *NoteMap) Lookup(keys []string, order DateOrder) (string, bool) {
	if nm == nil {
		return "", false
	}
	parts := make([]string, len(nm.KeyPos))
	for i, p := range nm.KeyPos {
		parts[i] = CompareKey(keys[p], order)
	}
	n, ok := nm.Values[strings.Join(parts, "\x1f")]
	return n, ok
}
