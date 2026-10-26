package main

import (
	"os"
	"strings"
	"testing"
)

func loadDDL(t *testing.T, path string) *TableDef {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := ParseDDL(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("want 1 table, got %d", len(tables))
	}
	return tables[0]
}

func col(t *testing.T, td *TableDef, name string) Column {
	for _, c := range td.Columns {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	t.Fatalf("no column %s", name)
	return Column{}
}

func TestParseOracleDDL(t *testing.T) {
	td := loadDDL(t, "sample/ddl_oracle.sql")
	if td.Athena || td.Schema != "TESTDATA" || td.Name != "GD_TRANS" || len(td.Columns) != 13 {
		t.Fatalf("%+v", td)
	}
	cases := map[string]Kind{"TRANS_CODE": KString, "STATEMENT_NUM": KDecimal, "SEQ_NO": KInteger, "QTY": KInteger,
		"RATE": KDecimal, "TXN_DATE": KDate, "CREATED_AT": KTimestamp, "UPDATED_AT": KTimestamp, "DESCRIPTION": KString}
	for n, k := range cases {
		if c := col(t, td, n); c.Kind != k {
			t.Errorf("%s: kind %v type %q", n, c.Kind, c.Type)
		}
	}
	if c := col(t, td, "TRANS_CODE"); !c.NotNull || c.Length != 16 || !c.ByteLen || !c.Quoted {
		t.Errorf("%+v", c)
	}
	if c := col(t, td, "NOTE_TEXT"); c.ByteLen || c.Length != 20 {
		t.Errorf("CHAR semantics: %+v", c)
	}
	if c := col(t, td, "STATEMENT_NUM"); c.Precision != 16 || c.Scale != 5 {
		t.Errorf("%+v", c)
	}
	if c := col(t, td, "AMOUNT"); !c.HasDef {
		t.Error("default")
	}
	if c := col(t, td, "UPDATED_AT"); c.Type != "TIMESTAMP(6) WITH TIME ZONE" {
		t.Errorf("%q", c.Type)
	}
	if !col(t, td, "DESCRIPTION").CLOB {
		t.Error("clob")
	}
}

func TestParseAthenaDDL(t *testing.T) {
	td := loadDDL(t, "sample/ddl_athena.sql")
	if !td.Athena || td.Schema != "test_db" || td.Name != "gd_trans" || len(td.Columns) != 12 {
		t.Fatalf("%+v %d", td, len(td.Columns))
	}
	if c := col(t, td, "ds"); !c.Partition || c.Kind != KString {
		t.Errorf("%+v", c)
	}
	if c := col(t, td, "extra"); c.Kind != KComplex || c.Type != "struct<a:int,b:string>" {
		t.Errorf("%+v", c)
	}
	if c := col(t, td, "branch_code"); c.Length != 5 || c.ByteLen {
		t.Errorf("%+v", c)
	}
	if TableIdent(td, Athena) != `"test_db"."gd_trans"` {
		t.Error(TableIdent(td, Athena))
	}
}

func build(t *testing.T, ddl string, dl Dialect, mode int) (string, []Issue, GenStats) {
	td := loadDDL(t, ddl)
	csv, err := ReadCSV("sample/gd_trans.csv")
	if err != nil {
		t.Fatal(err)
	}
	o := GenOptions{Dialect: dl, OracleMode: mode, BatchRows: 100, Table: TableIdent(td, dl), Source: csv.Name(), TotalRows: len(csv.Rows)}
	pos := map[string]int{}
	for i, h := range csv.Headers {
		pos[strings.ToLower(h)] = i
	}
	for _, c := range td.Columns {
		if i, ok := pos[strings.ToLower(c.Name)]; ok {
			o.Cols, o.CSVIdx = append(o.Cols, c), append(o.CSVIdx, i)
		}
	}
	rows, issues := Prepare(csv, o)
	o.SkipBad = true
	sql, st := Generate(rows, o)
	return sql, issues, st
}

func TestOracleSQL(t *testing.T) {
	sql, issues, st := build(t, "sample/ddl_oracle.sql", Oracle, OraclePerRow)
	if st.Rows != 2 || st.Skipped != 1 || st.Statements != 2 {
		t.Fatalf("%+v\n%s", st, sql)
	}
	want := []string{
		`INSERT INTO "TESTDATA"."GD_TRANS" ("TRANS_CODE", "STATEMENT_NUM", "BRANCH_CODE"`,
		`'ABCD12345EFGH234', 12435678654.03001, 'HN001', 'Nguyễn Văn An', 'O''Brien', 1500000.50, 1, 10, 0.125, TO_DATE('2026-10-26','YYYY-MM-DD'), TO_TIMESTAMP('2026-10-26 08:30:00.12','YYYY-MM-DD HH24:MI:SS.FF'), TO_TIMESTAMP('2026-10-26 08:30:00','YYYY-MM-DD HH24:MI:SS'), 'Chuyển khoản'`,
		`'XYZ98765QWER', 12435678654.04001, 'HCM01', 'Trần Thị Bích', NULL, NULL, 2, NULL, NULL, TO_DATE('2026-10-27','YYYY-MM-DD'), TO_TIMESTAMP('2026-10-27','YYYY-MM-DD'), NULL, NULL`,
		"COMMIT;",
	}
	for _, w := range want {
		if !strings.Contains(sql, w) {
			t.Errorf("missing %s\n%s", w, sql)
		}
	}
	msgs := map[string]bool{}
	for _, is := range issues {
		if is.Line == 4 {
			msgs[is.Col+": "+is.Msg] = true
		}
	}
	for _, m := range []string{"STATEMENT_NUM: không phải số", "BRANCH_CODE: dài 7 byte, vượt CHAR(5 BYTE)",
		"QTY: không phải số nguyên", "TXN_DATE: không đọc được ngày/giờ", "AMOUNT: 3 chữ số thập phân, NUMBER(18,2) sẽ làm tròn"} {
		if !msgs[m] {
			t.Errorf("missing issue %q in %v", m, msgs)
		}
	}
}

func TestOracleInsertAll(t *testing.T) {
	sql, _, st := build(t, "sample/ddl_oracle.sql", Oracle, OracleInsertAll)
	if st.Statements != 1 || !strings.Contains(sql, "INSERT ALL\n  INTO \"TESTDATA\".\"GD_TRANS\"") || !strings.Contains(sql, "SELECT 1 FROM DUAL;") {
		t.Fatal(sql)
	}
}

func TestAthenaSQL(t *testing.T) {
	sql, issues, st := build(t, "sample/ddl_athena.sql", Athena, 0)
	if st.Rows != 2 || st.Statements != 1 {
		t.Fatalf("%+v %v\n%s", st, issues, sql)
	}
	for _, w := range []string{
		`INSERT INTO "test_db"."gd_trans" ("trans_code", "statement_num", "branch_code", "cust_name", "amount", "seq_no", "is_reversal", "tags", "extra", "txn_date", "created_at", "ds") VALUES`,
		`('ABCD12345EFGH234', 12435678654.03001, 'HN001', 'Nguyễn Văn An', 1500000.50, 1, true, ARRAY['a','b'], ROW(1,'x'), DATE '2026-10-26', TIMESTAMP '2026-10-26 08:30:00.12', '2026-10-26'),`,
		`('XYZ98765QWER', 12435678654.04001, 'HCM01', 'Trần Thị Bích', NULL, 2, false, NULL, NULL, DATE '2026-10-27', TIMESTAMP '2026-10-27 00:00:00', '2026-10-27');`,
	} {
		if !strings.Contains(sql, w) {
			t.Errorf("missing %s\n%s", w, sql)
		}
	}
}

func TestAthenaBatchBySize(t *testing.T) {
	c := Column{Name: "s", Kind: KString}
	rows := make([]PreparedRow, 3000)
	for i := range rows {
		rows[i] = PreparedRow{Line: i + 2, Vals: []string{quoteStr(strings.Repeat("x", 200))}}
	}
	sql, st := Generate(rows, GenOptions{Dialect: Athena, Table: `"t"`, Cols: []Column{c}, BatchRows: 1000})
	if st.Statements < 4 {
		t.Fatalf("expected size split, got %d", st.Statements)
	}
	for _, stmt := range strings.Split(sql, ";\n") {
		if len(stmt) > 256*1024 {
			t.Fatal("statement too large")
		}
	}
}

func TestEdgeLiterals(t *testing.T) {
	o := GenOptions{Dialect: Oracle}
	clob := strings.Repeat("ab'", 3000)
	lit, msg, _ := Literal(Column{Kind: KString, CLOB: true}, clob, o)
	if msg != "" || !strings.HasPrefix(lit, "TO_CLOB('") || strings.Count(lit, "TO_CLOB") < 5 {
		t.Errorf("clob %q %s", lit[:40], msg)
	}
	if _, msg, isErr := Literal(Column{Kind: KString, NotNull: true}, " ", o); !isErr || msg == "" {
		t.Error("not null")
	}
	if lit, _, _ := Literal(Column{Kind: KString}, "", GenOptions{Dialect: Athena, EmptyString: true}); lit != "''" {
		t.Error(lit)
	}
	if lit, _, _ := Literal(Column{Kind: KString}, "NULL", GenOptions{Dialect: Athena, EmptyString: true}); lit != "NULL" {
		t.Error(lit)
	}
	if lit, _, _ := Literal(Column{Kind: KInteger}, "12.00", o); lit != "12" {
		t.Error(lit)
	}
	if lit, _, _ := Literal(Column{Kind: KString, Length: 3, ByteLen: true}, "ấấ", o); lit == "" {
		t.Error(lit)
	}
	if _, msg, _ := Literal(Column{Kind: KString, Length: 3, ByteLen: true, Type: "VARCHAR2(3 BYTE)"}, "ấấ", o); !strings.Contains(msg, "6 byte") {
		t.Error(msg)
	}
	if lit, _, _ := Literal(Column{Kind: KString}, `="00123"`, o); lit != "'00123'" {
		t.Error(lit)
	}
}
