package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Dialect int

const (
	Oracle Dialect = iota
	Athena
)

var dialectName = map[Dialect]string{Oracle: "Oracle (DBeaver)", Athena: "Athena (AWS)"}

const (
	OraclePerRow    = iota // one INSERT per row
	OracleInsertAll        // INSERT ALL ... SELECT 1 FROM DUAL in batches
)

// GenOptions holds what the user chose before generating.
type GenOptions struct {
	Dialect     Dialect
	Table       string   // target table exactly as it goes into the SQL
	Cols        []Column // DDL columns to fill, in DDL order
	CSVIdx      []int    // CSV column index for each of Cols
	Order       DateOrder
	EmptyString bool // Athena: an empty cell in a string column becomes '' instead of NULL
	OracleMode  int
	BatchRows   int  // rows per statement for INSERT ALL / Athena VALUES
	SkipBad     bool // leave out rows that have errors
	Source      string
	TotalRows   int
}

// Issue is a problem found in one cell.
type Issue struct {
	Line  int // line number in the CSV file (header = 1)
	Col   string
	Value string
	Msg   string
	Err   bool // false = warning only
}

type PreparedRow struct {
	Line int
	Vals []string
	Bad  bool
}

var (
	reInt = regexp.MustCompile(`^[+-]?\d+(?:\.0*)?$`)
	reDec = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$`)
)

// Prepare turns every CSV row into SQL literals and collects issues.
func Prepare(t *Table, o GenOptions) ([]PreparedRow, []Issue) {
	var rows []PreparedRow
	var issues []Issue
	for i, r := range t.Rows {
		pr := PreparedRow{Line: i + 2, Vals: make([]string, len(o.Cols))}
		for j, c := range o.Cols {
			raw := ""
			if o.CSVIdx[j] >= 0 {
				raw = r[o.CSVIdx[j]]
			}
			lit, msg, isErr := Literal(c, raw, o)
			pr.Vals[j] = lit
			if msg != "" {
				issues = append(issues, Issue{Line: pr.Line, Col: c.Name, Value: raw, Msg: msg, Err: isErr})
				if isErr {
					pr.Bad = true
				}
			}
		}
		rows = append(rows, pr)
	}
	return rows, issues
}

func quoteStr(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Literal formats one value for the column. It returns the SQL text and,
// when something is off, a message and whether it is an error.
func Literal(c Column, raw string, o GenOptions) (string, string, bool) {
	raw = unwrapExcelText(raw)
	val := strings.TrimSpace(raw)
	nullWord := strings.EqualFold(val, "null")
	if val == "" || nullWord {
		if !nullWord && c.Kind == KString && o.Dialect == Athena && o.EmptyString {
			return "''", "", false
		}
		if c.NotNull && !c.HasDef {
			return "NULL", "cột NOT NULL nhưng giá trị trống", true
		}
		return "NULL", "", false
	}

	switch c.Kind {
	case KString, KUnknown:
		s := raw
		if c.Length > 0 {
			n := utf8.RuneCountInString(s)
			unit := "ký tự"
			if c.ByteLen {
				n, unit = len(s), "byte"
			}
			if n > c.Length {
				return quoteStr(s), fmt.Sprintf("dài %d %s, vượt %s", n, unit, c.Type), true
			}
		}
		if o.Dialect == Oracle && len(s) > 4000 {
			if c.CLOB {
				return oracleClob(s), "", false
			}
			return quoteStr(s), "chuỗi dài hơn 4000 byte, Oracle không nhận literal dài vậy", true
		}
		return quoteStr(s), "", false

	case KInteger:
		if !reInt.MatchString(val) {
			return quoteStr(val), "không phải số nguyên", true
		}
		v := strings.TrimPrefix(val, "+")
		if i := strings.IndexByte(v, '.'); i >= 0 {
			v = v[:i]
		}
		if c.Precision > 0 && len(strings.TrimLeft(strings.TrimPrefix(v, "-"), "0")) > c.Precision {
			return v, fmt.Sprintf("vượt %d chữ số của %s", c.Precision, c.Type), true
		}
		return v, "", false

	case KDecimal:
		if !reDec.MatchString(val) {
			return quoteStr(val), "không phải số", true
		}
		v := strings.TrimPrefix(val, "+")
		if c.Precision > 0 && !strings.ContainsAny(v, "eE") {
			ip, fp, _ := strings.Cut(strings.TrimPrefix(v, "-"), ".")
			ip = strings.TrimLeft(ip, "0")
			if len(ip) > c.Precision-c.Scale {
				return v, fmt.Sprintf("phần nguyên %d chữ số, vượt %s", len(ip), c.Type), true
			}
			if fp = strings.TrimRight(fp, "0"); len(fp) > c.Scale {
				return v, fmt.Sprintf("%d chữ số thập phân, %s sẽ làm tròn", len(fp), c.Type), false
			}
		}
		return v, "", false

	case KDate, KTimestamp:
		d, ok := normDate(val, o.Order)
		if !ok {
			return quoteStr(val), "không đọc được ngày/giờ", true
		}
		return dateLiteral(c, d, o.Dialect)

	case KBoolean:
		switch strings.ToLower(val) {
		case "true", "1", "y", "yes", "t":
			if o.Dialect == Oracle {
				return "TRUE", "", false
			}
			return "true", "", false
		case "false", "0", "n", "no", "f":
			if o.Dialect == Oracle {
				return "FALSE", "", false
			}
			return "false", "", false
		}
		return quoteStr(val), "không phải true/false", true

	case KComplex:
		return val, "", false
	}
	return quoteStr(raw), "", false
}

// dateLiteral formats a normalized "yyyy-mm-dd[ hh:mm:ss[.f]]" value.
func dateLiteral(c Column, d string, dl Dialect) (string, string, bool) {
	date, tm, hasTime := strings.Cut(d, " ")
	if dl == Athena {
		if c.Kind == KDate {
			if hasTime {
				return "DATE '" + date + "'", "cột DATE của Athena không lưu giờ, phần giờ bị bỏ", false
			}
			return "DATE '" + date + "'", "", false
		}
		if !hasTime {
			d += " 00:00:00"
		}
		return "TIMESTAMP '" + d + "'", "", false
	}
	fn := "TO_DATE"
	if c.Kind == KTimestamp {
		fn = "TO_TIMESTAMP"
	}
	switch {
	case !hasTime:
		return fmt.Sprintf("%s('%s','YYYY-MM-DD')", fn, d), "", false
	case strings.Contains(tm, "."):
		if c.Kind == KDate {
			d = d[:strings.IndexByte(d, '.')]
			return fmt.Sprintf("TO_DATE('%s','YYYY-MM-DD HH24:MI:SS')", d), "cột DATE không lưu phần lẻ của giây, bị bỏ", false
		}
		return fmt.Sprintf("TO_TIMESTAMP('%s','YYYY-MM-DD HH24:MI:SS.FF')", d), "", false
	default:
		return fmt.Sprintf("%s('%s','YYYY-MM-DD HH24:MI:SS')", fn, d), "", false
	}
}

// oracleClob splits a long text into TO_CLOB pieces that each stay under
// the 4000-byte literal limit, even when every quote is doubled.
func oracleClob(s string) string {
	var parts []string
	for len(s) > 0 {
		n := 0
		for i := range s {
			if i > 1900 {
				break
			}
			n = i
		}
		if len(s) <= 1900 {
			n = len(s)
		}
		parts = append(parts, "TO_CLOB("+quoteStr(s[:n])+")")
		s = s[n:]
	}
	return strings.Join(parts, " || ")
}

// Ident quotes an identifier the way the target expects.
func Ident(c Column, dl Dialect) string {
	if dl == Athena || c.Quoted {
		return `"` + c.Name + `"`
	}
	return c.Name
}

// TableIdent builds the default target name from the DDL.
func TableIdent(t *TableDef, dl Dialect) string {
	q := func(name string, quoted bool) string {
		if dl == Athena || quoted {
			return `"` + name + `"`
		}
		return name
	}
	if t.Schema != "" {
		return q(t.Schema, t.SchemaQuoted) + "." + q(t.Name, t.NameQuoted)
	}
	return q(t.Name, t.NameQuoted)
}

// GenStats reports what went into the SQL file.
type GenStats struct {
	Rows, Skipped, Statements int
}

const athenaMaxBytes = 200_000 // Athena rejects query strings over 256 KB

// Generate renders the SQL script.
func Generate(rows []PreparedRow, o GenOptions) (string, GenStats) {
	var st GenStats
	var b strings.Builder
	cols := make([]string, len(o.Cols))
	for i, c := range o.Cols {
		cols[i] = Ident(c, o.Dialect)
	}
	colList := strings.Join(cols, ", ")

	fmt.Fprintf(&b, "-- Sinh bởi insertgen %s lúc %s\n", version, time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "-- Nguồn: %s (%d dòng) | Bảng: %s | %s\n", o.Source, o.TotalRows, o.Table, dialectName[o.Dialect])

	var keep []PreparedRow
	for _, r := range rows {
		if r.Bad && o.SkipBad {
			st.Skipped++
			continue
		}
		keep = append(keep, r)
	}
	if st.Skipped > 0 {
		var lines []string
		for _, r := range rows {
			if r.Bad {
				lines = append(lines, fmt.Sprint(r.Line))
			}
		}
		fmt.Fprintf(&b, "-- Bỏ qua %d dòng lỗi (dòng trong CSV): %s\n", st.Skipped, shorten(strings.Join(lines, ", "), 300))
	}
	st.Rows = len(keep)
	tuple := func(r PreparedRow) string { return "(" + strings.Join(r.Vals, ", ") + ")" }
	batch := o.BatchRows
	if batch <= 0 {
		batch = 100
	}

	switch {
	case o.Dialect == Oracle && o.OracleMode == OraclePerRow:
		b.WriteString("\n")
		for _, r := range keep {
			fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES %s;\n", o.Table, colList, tuple(r))
			st.Statements++
		}
		b.WriteString("\nCOMMIT;\n")

	case o.Dialect == Oracle:
		for i := 0; i < len(keep); i += batch {
			end := min(i+batch, len(keep))
			fmt.Fprintf(&b, "\n-- Lô %d: %d dòng\nINSERT ALL\n", st.Statements+1, end-i)
			for _, r := range keep[i:end] {
				fmt.Fprintf(&b, "  INTO %s (%s) VALUES %s\n", o.Table, colList, tuple(r))
			}
			b.WriteString("SELECT 1 FROM DUAL;\n")
			st.Statements++
		}
		b.WriteString("\nCOMMIT;\n")

	default:
		b.WriteString("-- Athena chạy 1 câu lệnh mỗi lần: bôi đen từng lô rồi bấm Run.\n")
		head := fmt.Sprintf("INSERT INTO %s (%s) VALUES\n", o.Table, colList)
		for i := 0; i < len(keep); {
			var body []string
			size := len(head)
			j := i
			for ; j < len(keep) && j-i < batch; j++ {
				t := "  " + tuple(keep[j])
				if len(body) > 0 && size+len(t)+2 > athenaMaxBytes {
					break
				}
				body = append(body, t)
				size += len(t) + 2
			}
			fmt.Fprintf(&b, "\n-- Lô %d: %d dòng\n%s%s;\n", st.Statements+1, j-i, head, strings.Join(body, ",\n"))
			st.Statements++
			i = j
		}
	}
	return b.String(), st
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
