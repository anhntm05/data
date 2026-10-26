package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/huh"
)

const version = "1.1.0"

func main() {
	fmt.Println()
	fmt.Println(titleStyle.Render("insertgen - Sinh câu lệnh INSERT từ DDL + CSV  v" + version))
	fmt.Println(dimStyle.Render("Dùng ↑/↓ để chọn, Space để tick, Enter để xác nhận, / để tìm, Ctrl+C để thoát."))
	fmt.Println()

	var ddl *TableDef
	for {
		var err error
		ddl, err = session(ddl)
		if errors.Is(err, errAbort) || errors.Is(err, huh.ErrUserAborted) {
			fmt.Println(dimStyle.Render("Đã thoát."))
			return
		}
		if err != nil {
			fmt.Println(errStyle.Render("Lỗi: ") + err.Error())
		}
		next := "same"
		opts := []huh.Option[string]{huh.NewOption("Dùng DDL khác", "new"), huh.NewOption("Thoát", "quit")}
		if ddl != nil {
			opts = append([]huh.Option[string]{huh.NewOption("Sinh tiếp với cùng bảng "+ddl.FullName()+" (CSV khác)", "same")}, opts...)
		} else {
			next = "new"
		}
		if ask(huh.NewSelect[string]().Title("Làm gì tiếp?").Options(opts...).Value(&next)) != nil || next == "quit" {
			return
		}
		if next == "new" {
			ddl = nil
		}
		fmt.Println()
	}
}

// session runs one generation. It returns the DDL used so the next round
// can reuse it.
func session(prev *TableDef) (*TableDef, error) {
	ddl := prev
	var err error
	if ddl == nil {
		if ddl, err = pickDDL(); err != nil {
			return nil, err
		}
	}
	showTable(ddl)

	o := GenOptions{Dialect: Oracle, Order: DMY, BatchRows: 100}
	if ddl.Athena {
		o.Dialect = Athena
	}
	if err := ask(huh.NewSelect[Dialect]().Title("Sinh câu lệnh cho").
		Options(huh.NewOption("Oracle - chạy trong DBeaver", Oracle), huh.NewOption("Athena - AWS", Athena)).
		Value(&o.Dialect)); err != nil {
		return ddl, err
	}

	csvPath, err := pickFile("Chọn file CSV dữ liệu", []string{".csv"}, "", strings.ToLower(ddl.Name))
	if err != nil {
		return ddl, err
	}
	t, err := ReadCSV(csvPath)
	if err != nil {
		return ddl, err
	}
	o.Source, o.TotalRows = t.Name(), len(t.Rows)

	if err := matchColumns(ddl, t, &o); err != nil {
		return ddl, err
	}
	if hasAmbiguousDates(t, o) {
		if err := ask(huh.NewSelect[DateOrder]().
			Title("Có ngày dạng 03/04/2026. Ngày tháng trong CSV viết theo thứ tự nào?").
			Options(huh.NewOption("dd/mm/yyyy  (03/04/2026 = 03 tháng 04)", DMY),
				huh.NewOption("mm/dd/yyyy  (03/04/2026 = 04 tháng 03)", MDY)).
			Value(&o.Order)); err != nil {
			return ddl, err
		}
	}

	o.Table = TableIdent(ddl, o.Dialect)
	fields := []huh.Field{huh.NewInput().Title("Tên bảng trong câu INSERT").
		Description("Sửa nếu schema/database ở môi trường test khác với DDL.").Value(&o.Table)}
	if o.Dialect == Oracle {
		fields = append(fields, huh.NewSelect[int]().Title("Kiểu câu lệnh").Options(
			huh.NewOption("Mỗi dòng 1 INSERT (dễ tìm dòng lỗi, chạy bằng Alt+X)", OraclePerRow),
			huh.NewOption("INSERT ALL theo lô 100 dòng (chạy nhanh hơn)", OracleInsertAll)).Value(&o.OracleMode))
	} else {
		fields = append(fields,
			huh.NewSelect[bool]().Title("Ô trống ở cột chuỗi").Options(
				huh.NewOption("NULL", false), huh.NewOption("'' (chuỗi rỗng)", true)).Value(&o.EmptyString),
			huh.NewSelect[int]().Title("Số dòng tối đa mỗi câu INSERT").
				Description("Athena giới hạn 256 KB mỗi câu lệnh, tool tự chia nhỏ hơn nếu cần.").
				Options(huh.NewOption("100", 100), huh.NewOption("500", 500), huh.NewOption("1000", 1000)).Value(&o.BatchRows))
	}
	if err := ask(fields...); err != nil {
		return ddl, err
	}
	o.Table = strings.TrimSpace(o.Table)
	if err := pickTransforms(&o, t); err != nil {
		return ddl, err
	}

	rows, issues := Prepare(t, o)
	if bad := showIssues(issues, rows); bad > 0 {
		choice := "skip"
		if err := ask(huh.NewSelect[string]().Title(fmt.Sprintf("Có %d dòng lỗi. Xử lý thế nào?", bad)).Options(
			huh.NewOption("Bỏ các dòng lỗi, sinh các dòng còn lại", "skip"),
			huh.NewOption("Vẫn sinh tất cả (DB sẽ báo lỗi ở các dòng này)", "all"),
			huh.NewOption("Huỷ để sửa CSV", "cancel")).Value(&choice)); err != nil {
			return ddl, err
		}
		if choice == "cancel" {
			return ddl, fmt.Errorf("đã huỷ")
		}
		o.SkipBad = choice == "skip"
	}

	suffix := map[Dialect]string{Oracle: "oracle", Athena: "athena"}[o.Dialect]
	out := filepath.Join(filepath.Dir(csvPath),
		strings.TrimSuffix(t.Name(), filepath.Ext(t.Name()))+"_insert_"+suffix+".sql")
	if err := ask(huh.NewInput().Title("Lưu file SQL tại").Value(&out)); err != nil {
		return ddl, err
	}
	out = CleanPath(out)
	if !strings.HasSuffix(strings.ToLower(out), ".sql") {
		out += ".sql"
	}

	sql, st := Generate(rows, o)
	if err := os.WriteFile(out, []byte(sql), 0o644); err != nil {
		return ddl, err
	}
	showResult(sql, st, out)

	var copyIt bool
	if len(sql) < 5<<20 {
		if ask(huh.NewConfirm().Title("Copy toàn bộ SQL vào clipboard?").Affirmative("Copy").Negative("Không").Value(&copyIt)) == nil && copyIt {
			if err := clipboard.WriteAll(sql); err != nil {
				fmt.Println(errStyle.Render("Không copy được: ") + err.Error())
			} else {
				fmt.Println(okStyle.Render("✓ Đã copy vào clipboard."))
			}
		}
	}
	if runtime.GOOS == "windows" {
		var open bool
		if ask(huh.NewConfirm().Title("Mở thư mục chứa file SQL?").Affirmative("Mở").Negative("Không").Value(&open)) == nil && open {
			exec.Command("explorer", "/select,", out).Start()
		}
	}
	return ddl, nil
}

func pickDDL() (*TableDef, error) {
	lib := ListDDL()
	mode := "file"
	var opts []huh.Option[string]
	if len(lib) > 0 {
		mode = "lib"
		opts = append(opts, huh.NewOption(fmt.Sprintf("Bảng đã lưu (%d bảng)", len(lib)), "lib"))
	}
	opts = append(opts, huh.NewOption("File .sql / .ddl / .txt", "file"), huh.NewOption("Dán DDL vào đây", "paste"))
	if err := ask(huh.NewSelect[string]().Title("Lấy DDL từ đâu?").Options(opts...).Value(&mode)); err != nil {
		return nil, err
	}
	if mode == "lib" {
		var items []huh.Option[string]
		for _, e := range lib {
			items = append(items, huh.NewOption(e.Name+dimStyle.Render("  dùng lần cuối "+e.Mod.Format("2006-01-02 15:04")), e.Path))
		}
		p := lib[0].Path
		if err := ask(huh.NewSelect[string]().Title("Chọn bảng").Description("Gõ / để tìm theo tên bảng.").
			Options(items...).Value(&p).Height(min(len(items)+3, 16))); err != nil {
			return nil, err
		}
		return LoadDDL(p)
	}
	var text string
	if mode == "paste" {
		if err := ask(huh.NewText().Title("Dán câu lệnh CREATE TABLE").
			Description("Ctrl+V để dán, Tab rồi Enter để xác nhận.").
			Lines(14).CharLimit(0).Value(&text)); err != nil {
			return nil, err
		}
	} else {
		p, err := pickFile("Chọn file DDL", []string{".sql", ".ddl", ".txt"}, "", "ddl")
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		text = strings.TrimPrefix(string(b), "\xef\xbb\xbf")
	}
	tables, err := ParseDDL(text)
	if err != nil {
		return nil, err
	}
	saveToLibrary(tables)
	if len(tables) == 1 {
		return tables[0], nil
	}
	var topts []huh.Option[int]
	for i, t := range tables {
		topts = append(topts, huh.NewOption(fmt.Sprintf("%s (%d cột)", t.FullName(), len(t.Columns)), i))
	}
	pick := 0
	if err := ask(huh.NewSelect[int]().Title("DDL có nhiều bảng, chọn 1 bảng").Options(topts...).Value(&pick)); err != nil {
		return nil, err
	}
	return tables[pick], nil
}

// saveToLibrary stores every parsed table so it can be picked next time.
func saveToLibrary(tables []*TableDef) {
	var saved, updated []string
	for _, t := range tables {
		_, replaced, err := SaveDDL(t)
		switch {
		case err != nil:
			fmt.Println(errStyle.Render("Không lưu được DDL "+t.FullName()+": ") + err.Error())
		case replaced:
			updated = append(updated, t.FullName())
		default:
			saved = append(saved, t.FullName())
		}
	}
	if len(saved) > 0 {
		fmt.Println(okStyle.Render("✓ Đã lưu DDL: ") + strings.Join(saved, ", ") + dimStyle.Render("  ("+libDir()+")"))
	}
	if len(updated) > 0 {
		fmt.Println(okStyle.Render("✓ Đã cập nhật DDL mới cho: ") + strings.Join(updated, ", "))
	}
}

func showTable(t *TableDef) {
	kind := "Oracle"
	if t.Athena {
		kind = "Athena / Hive"
	}
	lines := []string{fmt.Sprintf("%s  %s  (%d cột, DDL kiểu %s)", boldStyle.Render("Bảng:"), t.FullName(), len(t.Columns), kind)}
	var unknown, part, notnull []string
	var cols []string
	for _, c := range t.Columns {
		cols = append(cols, c.Name+" "+dimStyle.Render(strings.ToLower(c.Type)))
		switch {
		case c.Kind == KUnknown:
			unknown = append(unknown, c.Name+" "+c.Type)
		}
		if c.Partition {
			part = append(part, c.Name)
		}
		if c.NotNull {
			notnull = append(notnull, c.Name)
		}
	}
	lines = append(lines, columnsGrid(cols, 14))
	if len(part) > 0 {
		lines = append(lines, "Cột partition: "+strings.Join(part, ", "))
	}
	if len(notnull) > 0 {
		lines = append(lines, "NOT NULL: "+strings.Join(notnull, ", "))
	}
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
	if len(unknown) > 0 {
		fmt.Println(box(warnStyle, "⚠ Kiểu dữ liệu chưa hỗ trợ, sẽ ghi như chuỗi: "+strings.Join(unknown, ", ")))
	}
	fmt.Println()
}

// columnsGrid lays out entries in as many columns as fit in ~100
// characters, showing at most maxRows rows.
func columnsGrid(items []string, maxRows int) string {
	width := 0
	for _, it := range items {
		if w := len([]rune(stripANSI(it))); w > width {
			width = w
		}
	}
	n := max(1, min(3, 100/(width+3)))
	extra := 0
	if len(items) > maxRows*n {
		extra = len(items) - maxRows*n
		items = items[:maxRows*n]
	}
	var b strings.Builder
	for i, it := range items {
		if i > 0 && i%n == 0 {
			b.WriteString("\n")
		}
		b.WriteString(it)
		if i%n != n-1 && i != len(items)-1 {
			b.WriteString(strings.Repeat(" ", width-len([]rune(stripANSI(it)))+3))
		}
	}
	if extra > 0 {
		fmt.Fprintf(&b, "\n... và %d cột nữa", extra)
	}
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// matchColumns pairs DDL columns with CSV headers (case-insensitive) and
// lets the user untick columns to leave out.
func matchColumns(ddl *TableDef, t *Table, o *GenOptions) error {
	csvPos := map[string]int{}
	for i, h := range t.Headers {
		csvPos[strings.ToLower(strings.Trim(h, "\"`"))] = i
	}
	used := map[int]bool{}
	var matched, missing, missingReq []string
	idx := map[string]int{}
	for _, c := range ddl.Columns {
		if i, ok := csvPos[strings.ToLower(c.Name)]; ok {
			matched = append(matched, c.Name)
			idx[c.Name] = i
			used[i] = true
		} else {
			missing = append(missing, c.Name)
			if c.NotNull && !c.HasDef {
				missingReq = append(missingReq, c.Name)
			}
		}
	}
	var extra []string
	for i, h := range t.Headers {
		if !used[i] {
			extra = append(extra, h)
		}
	}
	info := fmt.Sprintf("%s %s  (%d dòng, %d cột)\nKhớp với DDL: %d/%d cột",
		boldStyle.Render("CSV:"), t.Name(), len(t.Rows), len(t.Headers), len(matched), len(ddl.Columns))
	fmt.Println(box(boxStyle, info))
	if len(extra)+len(missing) > 0 {
		var b strings.Builder
		b.WriteString(boldStyle.Render("⚠ Cột không khớp giữa DDL và CSV (so tên không phân biệt hoa thường)"))
		if len(extra) > 0 {
			b.WriteString("\n  Có trong CSV, không có trong DDL (bỏ qua): " + strings.Join(extra, ", "))
		}
		if len(missing) > 0 {
			b.WriteString("\n  Có trong DDL, không có trong CSV (không đưa vào INSERT): " + strings.Join(missing, ", "))
		}
		if len(missingReq) > 0 {
			b.WriteString("\n  " + errStyle.Render("Cột NOT NULL không có default bị thiếu, INSERT sẽ lỗi: ") + strings.Join(missingReq, ", "))
		}
		fmt.Println(box(warnStyle, b.String()))
	}
	if len(matched) == 0 {
		return fmt.Errorf("không có cột nào của CSV trùng tên với DDL")
	}
	chosen := append([]string{}, matched...)
	if err := ask(huh.NewMultiSelect[string]().Title("Các cột đưa vào câu INSERT").
		Description("Mặc định tất cả cột khớp. Bỏ tick cột muốn để DB tự điền (default, sequence...).").
		Options(huh.NewOptions(matched...)...).Value(&chosen).Filterable(true).
		Height(min(len(matched)+4, 16)).
		Validate(func(v []string) error {
			if len(v) == 0 {
				return errors.New("chọn ít nhất 1 cột")
			}
			return nil
		})); err != nil {
		return err
	}
	chosen = keepOrder(matched, chosen)
	byName := map[string]Column{}
	for _, c := range ddl.Columns {
		byName[c.Name] = c
	}
	for _, n := range chosen {
		o.Cols = append(o.Cols, byName[n])
		o.CSVIdx = append(o.CSVIdx, idx[n])
	}
	fmt.Println()
	return nil
}

func hasAmbiguousDates(t *Table, o GenOptions) bool {
	for j, c := range o.Cols {
		if c.Kind != KDate && c.Kind != KTimestamp {
			continue
		}
		for i, r := range t.Rows {
			if i > 500 {
				break
			}
			if LooksAmbiguousDate(r[o.CSVIdx[j]]) {
				return true
			}
		}
	}
	return false
}

// showIssues prints a grouped summary and returns the number of bad rows.
func showIssues(issues []Issue, rows []PreparedRow) int {
	bad := 0
	for _, r := range rows {
		if r.Bad {
			bad++
		}
	}
	if len(issues) == 0 {
		fmt.Println(okStyle.Render(fmt.Sprintf("✓ Dữ liệu hợp lệ: %d dòng, không có lỗi.", len(rows))))
		fmt.Println()
		return 0
	}
	type group struct {
		col, msg string
		err      bool
		n        int
		first    Issue
	}
	groups := map[string]*group{}
	var order []string
	nerr, nwarn := 0, 0
	for _, is := range issues {
		if is.Err {
			nerr++
		} else {
			nwarn++
		}
		msg := is.Msg
		if strings.HasPrefix(msg, "dài ") || strings.HasPrefix(msg, "phần nguyên ") || strings.Contains(msg, "chữ số thập phân") {
			msg = msg[strings.Index(msg, ", ")+2:] // group "dài 60 byte, vượt X" by "vượt X"
		}
		k := is.Col + "\x00" + msg
		if groups[k] == nil {
			groups[k] = &group{col: is.Col, msg: msg, err: is.Err, first: is}
			order = append(order, k)
		}
		groups[k].n++
	}
	sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].err && !groups[order[j]].err })
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d lỗi ở %d dòng, %d cảnh báo", boldStyle.Render("Kiểm tra dữ liệu:"), nerr, bad, nwarn)
	for i, k := range order {
		if i >= 12 {
			fmt.Fprintf(&b, "\n  ... và %d nhóm khác", len(order)-i)
			break
		}
		g := groups[k]
		tag := warnTag
		if g.err {
			tag = errTag
		}
		v := g.first.Value
		if len([]rune(v)) > 30 {
			v = string([]rune(v)[:30]) + "..."
		}
		fmt.Fprintf(&b, "\n  %s %s: %s (%d dòng, vd dòng %d = %q)", tag, g.col, g.msg, g.n, g.first.Line, v)
	}
	st := warnStyle
	fmt.Println(box(st, b.String()))
	fmt.Println()
	return bad
}

var (
	errTag  = errStyle.Render("LỖI")
	warnTag = dimStyle.Render("lưu ý")
)

func showResult(sql string, st GenStats, out string) {
	lines := []string{okStyle.Render("✓ Đã sinh SQL"),
		fmt.Sprintf("%d dòng → %d câu lệnh", st.Rows, st.Statements)}
	if st.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("Bỏ qua %d dòng lỗi", st.Skipped))
	}
	lines = append(lines, boldStyle.Render("File: ")+out, "", dimStyle.Render("Xem trước:"))
	n := 0
	for _, l := range strings.Split(sql, "\n") {
		if strings.HasPrefix(l, "--") || l == "" {
			continue
		}
		if len([]rune(l)) > 90 {
			l = string([]rune(l)[:90]) + "..."
		}
		lines = append(lines, l)
		if n++; n >= 6 {
			lines = append(lines, dimStyle.Render("..."))
			break
		}
	}
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
}

// pickTransforms lets the user add running-number suffixes to string
// columns and set fixed values, repeated until they are done.
func pickTransforms(o *GenOptions, t *Table) error {
	set := LoadSettings()
	for {
		desc := "Ví dụ: thêm hậu tố ANHTM01, ANHTM02... vào mã giao dịch, hoặc đổi business_date của mọi dòng sang 03/12/2025."
		if len(o.Transforms) > 0 {
			var l []string
			for _, tr := range o.Transforms {
				l = append(l, "• "+tr.Describe(o.Cols))
			}
			desc = "Đã chọn:\n" + strings.Join(l, "\n")
		}
		action := "done"
		if err := ask(huh.NewSelect[string]().Title("Biến đổi dữ liệu trước khi sinh INSERT?").Description(desc).Options(
			huh.NewOption("Xong, không biến đổi thêm", "done"),
			huh.NewOption("Thêm hậu tố tăng dần cho cột chuỗi (vd "+set.Suffix+"01, "+set.Suffix+"02...)", "suffix"),
			huh.NewOption("Đặt giá trị mới cho cả cột (vd đổi ngày)", "set")).Value(&action)); err != nil {
			return err
		}
		if action == "done" {
			fmt.Println()
			return nil
		}
		var colOpts []huh.Option[int]
		for i, c := range o.Cols {
			if action == "suffix" && c.Kind != KString {
				continue
			}
			sample := ""
			if len(t.Rows) > 0 {
				sample = strings.TrimSpace(t.Rows[0][o.CSVIdx[i]])
				if len([]rune(sample)) > 25 {
					sample = string([]rune(sample)[:25]) + "..."
				}
			}
			colOpts = append(colOpts, huh.NewOption(c.Name+dimStyle.Render("  "+strings.ToLower(c.Type)+"  vd: "+sample), i))
		}
		if len(colOpts) == 0 {
			fmt.Println(errStyle.Render("Không có cột chuỗi nào để thêm hậu tố."))
			continue
		}
		col := colOpts[0].Value
		if err := ask(huh.NewSelect[int]().Title("Chọn cột").Options(colOpts...).Value(&col).Height(min(len(colOpts)+3, 16))); err != nil {
			return err
		}
		c := o.Cols[col]
		if action == "suffix" {
			text, start, digits := set.Suffix, "1", fmt.Sprint(set.Digits)
			if err := ask(
				huh.NewInput().Title("Hậu tố").Value(&text).Validate(func(s string) error {
					if strings.ContainsAny(s, "'") {
						return errors.New("không dùng dấu '")
					}
					return nil
				}),
				huh.NewInput().Title("Bắt đầu từ số").Value(&start).Validate(isNumber),
				huh.NewInput().Title("Số chữ số (2 → 01, 02...)").Value(&digits).Validate(isNumber),
			); err != nil {
				return err
			}
			tr := Transform{Kind: TSuffix, Col: col, Text: text, Start: atoi(start), Digits: max(1, atoi(digits))}
			if last := tr.Start + len(t.Rows) - 1; len(fmt.Sprint(last)) > tr.Digits {
				fmt.Println(box(warnStyle, fmt.Sprintf("⚠ CSV có %d dòng nên số sẽ chạy tới %d, dài hơn %d chữ số.", len(t.Rows), last, tr.Digits)))
			}
			if c.Length > 0 {
				fmt.Println(dimStyle.Render(fmt.Sprintf("Lưu ý: %s là %s, giá trị sau khi thêm hậu tố vẫn được kiểm tra độ dài.", c.Name, c.Type)))
			}
			o.Transforms = append(o.Transforms, tr)
			set.Suffix, set.Digits = text, tr.Digits
			SaveSettings(set)
			continue
		}
		var val, match string
		hint := ""
		if c.Kind == KDate || c.Kind == KTimestamp {
			hint = "Ngày viết dạng nào cũng được: 03/12/2025, 2025-12-03..."
		}
		if err := ask(
			huh.NewInput().Title("Giá trị mới cho "+c.Name).Description(strings.TrimSpace(hint+" Để trống = NULL.")).Value(&val),
			huh.NewInput().Title("Chỉ đổi các dòng đang có giá trị (để trống = đổi tất cả)").
				Description("Vd 02/12/2025 để chỉ đổi những dòng có ngày đó.").Value(&match),
		); err != nil {
			return err
		}
		tr := Transform{Kind: TSet, Col: col, Text: strings.TrimSpace(val), Match: strings.TrimSpace(match)}
		if lit, msg, isErr := Literal(c, tr.Text, *o); isErr {
			fmt.Println(box(warnStyle, "⚠ Giá trị "+tr.Text+" không hợp lệ với cột "+c.Name+": "+msg))
			continue
		} else {
			fmt.Println(dimStyle.Render("Sẽ ghi vào SQL: " + lit))
		}
		o.Transforms = append(o.Transforms, tr)
	}
}

var reDigits = regexp.MustCompile(`^\d{1,6}$`)

func isNumber(s string) error {
	if !reDigits.MatchString(strings.TrimSpace(s)) {
		return errors.New("nhập số nguyên dương")
	}
	return nil
}
