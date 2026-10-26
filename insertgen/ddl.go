package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Kind is the value category of a column, used to format literals.
type Kind int

const (
	KString Kind = iota
	KInteger
	KDecimal
	KDate
	KTimestamp
	KBoolean
	KComplex // Athena array / map / struct: values are passed through as written
	KUnknown
)

var kindName = map[Kind]string{
	KString: "chuỗi", KInteger: "số nguyên", KDecimal: "số thập phân", KDate: "ngày",
	KTimestamp: "ngày giờ", KBoolean: "boolean", KComplex: "kiểu phức hợp", KUnknown: "không rõ",
}

// Column is one column of a CREATE TABLE statement.
type Column struct {
	Name      string // as written, without quotes
	Quoted    bool   // written as "Name" (Oracle) - keep quotes when generating
	Type      string // full type text, e.g. VARCHAR2(50 BYTE)
	Kind      Kind
	Length    int  // max length for character types, 0 if none
	ByteLen   bool // Oracle BYTE semantics: Length counts UTF-8 bytes
	Precision int  // for NUMBER/DECIMAL, 0 if none
	Scale     int
	NotNull   bool
	HasDef    bool // has a DEFAULT clause
	Partition bool // Athena PARTITIONED BY column
	CLOB      bool
}

// TableDef is a parsed CREATE TABLE.
type TableDef struct {
	Schema, Name             string
	SchemaQuoted, NameQuoted bool
	Columns                  []Column
	Athena                   bool // looks like Athena / Hive DDL
}

func (t *TableDef) FullName() string {
	if t.Schema != "" {
		return t.Schema + "." + t.Name
	}
	return t.Name
}

var (
	reCreate = regexp.MustCompile(`(?is)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:GLOBAL\s+TEMPORARY\s+|EXTERNAL\s+|PRIVATE\s+TEMPORARY\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)
	reArgs   = regexp.MustCompile(`^\(\s*(\*|\d+)?\s*(?:,\s*(-?\d+))?\s*(BYTE|CHAR)?\s*\)`)
)

// ParseDDL returns every CREATE TABLE found in the text.
func ParseDDL(text string) ([]*TableDef, error) {
	src := stripComments(text)
	var out []*TableDef
	locs := reCreate.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return nil, fmt.Errorf("không tìm thấy câu lệnh CREATE TABLE trong DDL")
	}
	for i, loc := range locs {
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		t, err := parseTable(src[loc[1]:end], src[loc[0]:loc[1]])
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func parseTable(s, head string) (*TableDef, error) {
	t := &TableDef{Athena: strings.Contains(strings.ToUpper(head), "EXTERNAL")}
	s = strings.TrimSpace(s)
	var parts []string
	var quoted []bool
	for {
		id, q, rest := readIdent(s)
		if id == "" {
			return nil, fmt.Errorf("không đọc được tên bảng trong DDL")
		}
		parts, quoted = append(parts, id), append(quoted, q)
		s = strings.TrimSpace(rest)
		if !strings.HasPrefix(s, ".") {
			break
		}
		s = strings.TrimSpace(s[1:])
	}
	n := len(parts)
	t.Name, t.NameQuoted = parts[n-1], quoted[n-1]
	if n > 1 {
		t.Schema, t.SchemaQuoted = parts[n-2], quoted[n-2]
	}
	if !strings.HasPrefix(s, "(") {
		return nil, fmt.Errorf("bảng %s: thiếu danh sách cột sau tên bảng", t.FullName())
	}
	body, rest, err := matchParen(s)
	if err != nil {
		return nil, fmt.Errorf("bảng %s: %w", t.FullName(), err)
	}
	if strings.Contains(s[:len(s)-len(rest)], "`") {
		t.Athena = true
	}
	cols, err := parseColumns(body, false)
	if err != nil {
		return nil, fmt.Errorf("bảng %s: %w", t.FullName(), err)
	}
	t.Columns = cols
	if m := regexp.MustCompile(`(?is)\bPARTITIONED\s+BY\s*\(`).FindStringIndex(rest); m != nil {
		t.Athena = true
		pbody, _, err := matchParen(rest[m[1]-1:])
		if err == nil {
			pcols, err := parseColumns(pbody, true)
			if err != nil {
				return nil, fmt.Errorf("bảng %s (PARTITIONED BY): %w", t.FullName(), err)
			}
			t.Columns = append(t.Columns, pcols...)
		}
	}
	if !t.Athena {
		for _, c := range t.Columns {
			u := strings.ToUpper(c.Type)
			if strings.HasPrefix(u, "STRING") || strings.HasPrefix(u, "ARRAY") || strings.HasPrefix(u, "MAP") || strings.HasPrefix(u, "STRUCT") {
				t.Athena = true
				break
			}
		}
	}
	if len(t.Columns) == 0 {
		return nil, fmt.Errorf("bảng %s không có cột nào", t.FullName())
	}
	if t.Athena {
		for i := range t.Columns {
			t.Columns[i].ByteLen = false
		}
	}
	return t, nil
}

var skipItem = map[string]bool{"CONSTRAINT": true, "PRIMARY": true, "UNIQUE": true, "FOREIGN": true,
	"CHECK": true, "INDEX": true, "KEY": true, "SUPPLEMENTAL": true, "PERIOD": true, "LIKE": true}

var stopWords = map[string]bool{"NOT": true, "NULL": true, "DEFAULT": true, "CONSTRAINT": true,
	"COMMENT": true, "PRIMARY": true, "UNIQUE": true, "CHECK": true, "REFERENCES": true,
	"ENABLE": true, "DISABLE": true, "GENERATED": true, "COLLATE": true, "VISIBLE": true,
	"INVISIBLE": true, "ENCRYPT": true, "AS": true}

func parseColumns(body string, partition bool) ([]Column, error) {
	var cols []Column
	for _, item := range splitTop(body) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		first := strings.ToUpper(strings.FieldsFunc(item, func(r rune) bool { return unicode.IsSpace(r) || r == '(' })[0])
		if skipItem[first] {
			continue
		}
		name, q, rest := readIdent(item)
		if name == "" {
			return nil, fmt.Errorf("không đọc được cột: %s", item)
		}
		c := Column{Name: name, Quoted: q, Partition: partition}
		c.Type, rest = readType(strings.TrimSpace(rest))
		mods := strings.ToUpper(rest)
		c.NotNull = regexp.MustCompile(`\bNOT\s+NULL\b`).MatchString(mods) || strings.Contains(mods, "PRIMARY KEY")
		c.HasDef = regexp.MustCompile(`\bDEFAULT\b`).MatchString(mods)
		classify(&c)
		cols = append(cols, c)
	}
	return cols, nil
}

// readType reads type words and their (args) / <args> up to a modifier keyword.
func readType(s string) (string, string) {
	var b strings.Builder
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		if s == "" {
			break
		}
		switch s[0] {
		case '(':
			body, rest, err := matchParen(s)
			if err != nil {
				return strings.TrimSpace(b.String() + s), ""
			}
			b.WriteString("(" + body + ")")
			s = rest
			continue
		case '<':
			depth := 0
			i := 0
			for ; i < len(s); i++ {
				if s[i] == '<' {
					depth++
				} else if s[i] == '>' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if i >= len(s) {
				i = len(s) - 1
			}
			b.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		end := strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '(' || r == '<' })
		if end < 0 {
			end = len(s)
		}
		w := s[:end]
		if stopWords[strings.ToUpper(w)] {
			break
		}
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(w)
		s = s[end:]
	}
	return strings.TrimSpace(b.String()), s
}

func classify(c *Column) {
	u := strings.ToUpper(c.Type)
	base := u
	if i := strings.IndexAny(base, "( <"); i >= 0 {
		base = base[:i]
	}
	var p, sc int
	hasArgs := false
	if i := strings.Index(u, "("); i >= 0 {
		if m := reArgs.FindStringSubmatch(strings.ReplaceAll(u[i:], " (", "(")); m != nil {
			hasArgs = true
			if m[1] != "" && m[1] != "*" {
				p, _ = strconv.Atoi(m[1])
			}
			if m[2] != "" {
				sc, _ = strconv.Atoi(m[2])
			}
		}
	}
	switch base {
	case "VARCHAR2", "VARCHAR", "CHAR", "CHARACTER":
		// Oracle counts bytes unless the type says (n CHAR); Athena counts
		// characters (fixed up in parseTable).
		c.Kind, c.Length = KString, p
		c.ByteLen = !strings.Contains(u, "CHAR)")
	case "NVARCHAR2", "NCHAR":
		c.Kind, c.Length = KString, p
	case "STRING", "LONG", "RAW", "ROWID", "UROWID", "XMLTYPE", "VARBINARY", "BINARY":
		c.Kind = KString
	case "CLOB", "NCLOB":
		c.Kind, c.CLOB = KString, true
	case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "PLS_INTEGER":
		c.Kind = KInteger
	case "NUMBER", "NUMERIC", "DECIMAL", "DEC":
		c.Precision, c.Scale = p, sc
		if hasArgs && sc == 0 {
			c.Kind = KInteger
		} else {
			c.Kind = KDecimal
		}
	case "FLOAT", "DOUBLE", "REAL", "BINARY_FLOAT", "BINARY_DOUBLE":
		c.Kind = KDecimal
	case "DATE":
		c.Kind = KDate
	case "TIMESTAMP", "DATETIME":
		c.Kind = KTimestamp
	case "BOOLEAN", "BOOL":
		c.Kind = KBoolean
	case "ARRAY", "MAP", "STRUCT", "ROW":
		c.Kind = KComplex
	default:
		c.Kind = KUnknown
	}
}

// readIdent reads "quoted", `quoted` or a bare identifier.
func readIdent(s string) (string, bool, string) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" {
		return "", false, s
	}
	if s[0] == '"' || s[0] == '`' || s[0] == '[' {
		closer := s[0]
		if closer == '[' {
			closer = ']'
		}
		if i := strings.IndexByte(s[1:], closer); i >= 0 {
			return s[1 : i+1], s[0] == '"', s[i+2:]
		}
		return "", false, s
	}
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' || r == '#')
	})
	if end < 0 {
		end = len(s)
	}
	return s[:end], false, s[end:]
}

// matchParen takes s starting with "(" and returns the inside and the rest.
func matchParen(s string) (string, string, error) {
	depth := 0
	var q byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if q != 0 {
			if ch == q {
				q = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			q = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], nil
			}
		}
	}
	return "", "", fmt.Errorf("thiếu dấu ) đóng danh sách cột")
}

// splitTop splits on commas that are not inside (), <> or quotes.
func splitTop(s string) []string {
	var out []string
	paren, angle := 0, 0
	var q byte
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if q != 0 {
			if ch == q {
				q = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			q = ch
		case '(':
			paren++
		case ')':
			paren--
		case '<':
			angle++
		case '>':
			if angle > 0 {
				angle--
			}
		case ',':
			if paren == 0 && angle == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func stripComments(s string) string {
	var b strings.Builder
	var q byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if q != 0 {
			b.WriteByte(ch)
			if ch == q {
				q = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			q = ch
			b.WriteByte(ch)
			continue
		}
		if ch == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '*' {
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 3
			} else {
				i = len(s)
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}
