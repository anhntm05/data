package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Table is a file loaded into memory: a header row and data rows.
type Table struct {
	Path    string
	Sheet   string // xlsx sheet name, empty for CSV
	Headers []string
	Rows    [][]string
}

func (t *Table) Name() string { return filepath.Base(t.Path) }

// CleanPath turns what the user typed or dragged into the terminal into a
// path: Windows drag-and-drop adds quotes, PowerShell may add "& '...'".
func CleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "& ")
	p = strings.TrimSpace(p)
	for len(p) >= 2 && (p[0] == '"' && p[len(p)-1] == '"' || p[0] == '\'' && p[len(p)-1] == '\'') {
		p = strings.TrimSpace(p[1 : len(p)-1])
	}
	return p
}

// ReadCSV reads a UTF-8 CSV (BOM optional). The delimiter is detected from
// the first line among comma, semicolon, tab and pipe.
func ReadCSV(path string) (*Table, error) {
	return LoadTable(path, "")
}

// LoadTable reads one table: a CSV file, or one sheet of an xlsx file.
func LoadTable(path, sheet string) (*Table, error) {
	rows, err := ReadSheetRows(path, sheet)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("file %s trống", filepath.Base(path))
	}
	t := &Table{Path: path, Sheet: sheet}
	for _, h := range rows[0] {
		t.Headers = append(t.Headers, strings.TrimSpace(h))
	}
	seen := map[string]bool{}
	for _, h := range t.Headers {
		if h == "" {
			return nil, fmt.Errorf("file %s có cột không có tên ở header", t.Name())
		}
		if seen[h] {
			return nil, fmt.Errorf("file %s có 2 cột trùng tên %q", t.Name(), h)
		}
		seen[h] = true
	}
	for _, r := range rows[1:] {
		if isBlankRow(r) {
			continue
		}
		row := make([]string, len(t.Headers))
		copy(row, r)
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

func readCSVRows(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	first, _, _ := bufio.NewReader(bytes.NewReader(data)).ReadLine()
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = detectDelimiter(string(first))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("lỗi đọc %s: %w", filepath.Base(path), err)
		}
		rows = append(rows, rec)
	}
	return rows, nil
}

func detectDelimiter(line string) rune {
	best, bestN := ',', 0
	for _, d := range []rune{',', ';', '\t', '|'} {
		if n := strings.Count(line, string(d)); n > bestN {
			best, bestN = d, n
		}
	}
	return best
}

func isBlankRow(r []string) bool {
	for _, v := range r {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// ReadAnyRows reads all rows of a CSV or the first sheet of an .xlsx,
// without assuming where the header is (used for reference files).
// SheetNames lists the sheets of an xlsx file. A CSV has one table, named
// after the file.
func SheetNames(path string) ([]string, error) {
	if !isXLSX(path) {
		return []string{filepath.Base(path)}, nil
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names := f.GetSheetList()
	if len(names) == 0 {
		return nil, fmt.Errorf("file %s không có sheet nào", filepath.Base(path))
	}
	return names, nil
}

func isXLSX(path string) bool {
	e := strings.ToLower(filepath.Ext(path))
	return e == ".xlsx" || e == ".xlsm"
}

// ReadSheetRows returns the cells of one sheet, or of a CSV file (sheet is
// ignored). Excel cells are read as displayed, so dates and numbers keep the
// format the user sees.
func ReadSheetRows(path, sheet string) ([][]string, error) {
	if !isXLSX(path) {
		return readCSVRows(path)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.GetRows(sheet)
}

// unwrapExcelText undoes the ="..." wrapping written to CSV output.
func unwrapExcelText(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `="`) && strings.HasSuffix(s, `"`) && len(s) >= 3 {
		return strings.ReplaceAll(s[2:len(s)-1], `""`, `"`)
	}
	return s
}
