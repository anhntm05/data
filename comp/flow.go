package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
)

// The output tabs that can take notes from a reference file.
var noteTabs = []string{sheetRecords, sheetFields}

func session(args []string) error {
	var uatPath, prodPath string
	var err error
	if len(args) >= 2 {
		uatPath, prodPath = CleanPath(args[0]), CleanPath(args[1])
		correct := true
		if err := ask(huh.NewConfirm().
			Title("Đã nhận 2 file").
			Description(fmt.Sprintf("UAT  = %s\nPROD = %s", filepath.Base(uatPath), filepath.Base(prodPath))).
			Affirmative("Đúng").Negative("Đảo lại").Value(&correct)); err != nil {
			return err
		}
		if !correct {
			uatPath, prodPath = prodPath, uatPath
		}
	} else {
		if uatPath, err = pickFile("Chọn file UAT (.csv / .xlsx)", []string{".csv", ".xlsx"}, "", "uat", false); err != nil {
			return err
		}
		if prodPath, err = pickFile("Chọn file PROD (.csv / .xlsx)", []string{".csv", ".xlsx"}, uatPath, "prod", false); err != nil {
			return err
		}
	}

	uat, err := loadInput(uatPath, "UAT")
	if err != nil {
		return err
	}
	prod, err := loadInput(prodPath, "PROD")
	if err != nil {
		return err
	}
	cc := CheckColumns(uat, prod)
	showFiles(uat, prod, cc)
	if len(cc.Common) == 0 {
		return fmt.Errorf("2 file không có cột nào trùng tên, không thể so sánh")
	}

	var opts Options
	opts.Order = DMY
	if hasAmbiguousDates(uat, prod, cc.Common) {
		if err := ask(huh.NewSelect[DateOrder]().
			Title("Có ngày dạng 03/04/2026. Ngày tháng trong file viết theo thứ tự nào?").
			Options(huh.NewOption("dd/mm/yyyy  (03/04/2026 = 03 tháng 04)", DMY),
				huh.NewOption("mm/dd/yyyy  (03/04/2026 = 04 tháng 03)", MDY)).
			Value(&opts.Order)); err != nil {
			return err
		}
	}

	// Key and column selection, repeated while the user wants to fix
	// duplicates or goes back from the column step to change the keys.
	for {
		if opts.Keys, err = pickKeys(cc.Common, opts.Keys); err != nil {
			return err
		}
		nonKeys := without(cc.Common, opts.Keys)
		retry, err := checkDuplicates(uat, prod, opts, nonKeys)
		if err != nil {
			return err
		}
		if retry {
			continue
		}
		opts.Compare = nonKeys
		next := "go"
		var fields []huh.Field
		if len(nonKeys) > 0 {
			fields = append(fields, huh.NewMultiSelect[string]().
				Title("Các cột sẽ được so sánh").
				Description("Mặc định so sánh tất cả. Bỏ tick cột muốn bỏ qua (vd: updated_at).").
				Options(huh.NewOptions(nonKeys...)...).
				Value(&opts.Compare).Filterable(true).Height(min(len(nonKeys)+4, 16)))
		}
		fields = append(fields, huh.NewSelect[string]().Title("Tiếp theo").Options(
			huh.NewOption("Tiếp tục", "go"),
			huh.NewOption("← Quay lại chọn key", "back")).Value(&next))
		if err := ask(fields...); err != nil {
			return err
		}
		if next == "back" {
			continue
		}
		opts.Compare = keepOrder(nonKeys, opts.Compare)
		break
	}

	refPath, notes, err := pickRefs(opts.Keys, opts.Order)
	if err != nil {
		return err
	}
	out, err := pickOutput(uat, prod)
	if err != nil {
		return err
	}

	showPlan(uat, prod, opts, refPath, notes, out)
	start := true
	if err := ask(huh.NewConfirm().Title("Bắt đầu so sánh?").Affirmative("Bắt đầu").Negative("Huỷ").Value(&start)); err != nil {
		return err
	}
	if !start {
		return fmt.Errorf("đã huỷ")
	}

	begin := time.Now()
	res := Compare(uat, prod, opts)
	if err := res.WriteXLSX(out, notes); err != nil {
		return fmt.Errorf("không ghi được file Excel: %w", err)
	}
	showResult(res, notes, out, time.Since(begin))
	return nil
}

// loadInput reads a CSV, or an xlsx file after the user picks its sheet.
func loadInput(path, who string) (*Table, error) {
	names, err := SheetNames(path)
	if err != nil {
		return nil, err
	}
	sheet := names[0]
	if len(names) > 1 {
		if err := ask(huh.NewSelect[string]().
			Title(fmt.Sprintf("Chọn sheet của file %s: %s", who, filepath.Base(path))).
			Options(huh.NewOptions(names...)...).Value(&sheet)); err != nil {
			return nil, err
		}
	}
	return LoadTable(path, sheet)
}

// pickRefs asks for a reference file and, for each output tab that takes
// notes, which of its sheets and columns to read. It returns the reference
// path and the note map per tab.
func pickRefs(keys []string, order DateOrder) (string, map[string]*NoteMap, error) {
	for {
		p, notes, err := pickRefOnce(keys, order)
		if errors.Is(err, errBack) {
			continue
		}
		return p, notes, err
	}
}

// pickRefOnce asks whether to use a reference, then for the file. It
// returns errBack when the user goes back from the file step.
func pickRefOnce(keys []string, order DateOrder) (string, map[string]*NoteMap, error) {
	use := false
	if err := ask(huh.NewConfirm().Title("Dùng file reference để lấy note?").
		Description("File reference (.xlsx hoặc .csv) có cột key và cột note.\nCó thể lấy note cho tab records và tab field_mismatch.").
		Affirmative("Có").Negative("Không").Value(&use)); err != nil || !use {
		return "", nil, err
	}
	for {
		p, err := pickFile("Chọn file reference (.xlsx / .csv)", []string{".xlsx", ".csv"}, "", "sosanh", true)
		if err != nil {
			return "", nil, err
		}
		names, err := SheetNames(p)
		if err != nil {
			fmt.Println(box(warnStyle, boldStyle.Render("⚠ Không đọc được file reference: ")+err.Error()))
			continue
		}
		notes := map[string]*NoteMap{}
		var lines []string
		for _, tab := range noteTabs {
			nm, err := pickTabNotes(p, names, tab, keys, order)
			if err != nil {
				lines = append(lines, boldStyle.Render(tab+": ")+err.Error())
				continue
			}
			if nm != nil {
				notes[tab] = nm
				lines = append(lines, fmt.Sprintf("%s: %d note, cột %q → cột %q", tab, len(nm.Values), nm.src, nm.Col))
			}
		}
		if len(notes) == 0 {
			retry := true
			if err := ask(huh.NewConfirm().Title("Không lấy được note từ file này. Chọn file khác?").
				Affirmative("Chọn file khác").Negative("Bỏ qua note").Value(&retry)); err != nil {
				return "", nil, err
			}
			if !retry {
				return "", nil, nil
			}
			continue
		}
		fmt.Println(box(boxStyle, okStyle.Render("✓ Reference: ")+filepath.Base(p)+"\n"+strings.Join(lines, "\n")))
		fmt.Println()
		return p, notes, nil
	}
}

// pickTabNotes maps one output tab to a sheet of the reference. A sheet with
// the same name as the tab is used automatically; otherwise the user picks.
func pickTabNotes(path string, names []string, tab string, keys []string, order DateOrder) (*NoteMap, error) {
	sheet := ""
	for _, n := range names {
		if strings.EqualFold(strings.TrimSpace(n), tab) {
			sheet = n
		}
	}
	if sheet == "" {
		options := []huh.Option[string]{huh.NewOption("Không lấy note cho tab này", "")}
		for _, n := range names {
			options = append(options, huh.NewOption(n, n))
		}
		if err := ask(huh.NewSelect[string]().
			Title(fmt.Sprintf("Tab %q lấy note từ sheet nào của file reference?", tab)).
			Description("Không có sheet trùng tên với tab này.").
			Options(options...).Value(&sheet)); err != nil {
			return nil, err
		}
		if sheet == "" {
			return nil, nil
		}
	}
	rows, err := ReadSheetRows(path, sheet)
	if err != nil {
		return nil, err
	}
	// field_mismatch has several lines per key, so match the field name too.
	match := keys
	if tab == sheetFields {
		match = append(append([]string{}, keys...), colField)
	}
	hi, hdr, err := FindHeader(rows, match)
	if err != nil {
		return nil, err
	}
	source := ""
	var sourceOpts []huh.Option[string]
	for _, h := range hdr {
		if h = strings.TrimSpace(h); h != "" {
			sourceOpts = append(sourceOpts, huh.NewOption(h, h))
			if source == "" || strings.EqualFold(h, "note") {
				source = h
			}
		}
	}
	target := "note"
	if err := ask(
		huh.NewSelect[string]().Title(fmt.Sprintf("Tab %q: lấy note từ cột nào của sheet %q?", tab, sheet)).
			Options(sourceOpts...).Value(&source),
		huh.NewInput().Title(fmt.Sprintf("Tab %q: note sẽ ghi vào cột tên là", tab)).
			Value(&target).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("tên cột không được để trống")
			}
			return nil
		}),
	); err != nil {
		return nil, err
	}
	nm, err := BuildNoteMap(rows, hi, source, strings.TrimSpace(target), match, order)
	if err != nil {
		return nil, err
	}
	nm.src = source
	if len(nm.Missing) > 0 {
		fmt.Println(box(warnStyle, boldStyle.Render("⚠ ")+fmt.Sprintf("Sheet %q không có cột key: %s. Note khớp theo các cột còn lại.",
			sheet, strings.Join(nm.Missing, ", "))))
	}
	return nm, nil
}

// pickOutput asks where to save the xlsx result. The default is next to
// the UAT file.
func pickOutput(uat, prod *Table) (string, error) {
	base := func(t *Table) string { return strings.TrimSuffix(t.Name(), filepath.Ext(t.Name())) }
	out := filepath.Join(filepath.Dir(uat.Path), fmt.Sprintf("comp_%s_vs_%s.xlsx", base(uat), base(prod)))
	if err := ask(huh.NewInput().Title("Lưu file kết quả tại (.xlsx)").Value(&out)); err != nil {
		return "", err
	}
	return withExt(CleanPath(out), ".xlsx"), nil
}

func showPlan(uat, prod *Table, o Options, ref string, notes map[string]*NoteMap, out string) {
	order := "dd/mm/yyyy"
	if o.Order == MDY {
		order = "mm/dd/yyyy"
	}
	refLine := "Reference  : (không dùng)"
	if ref != "" {
		refLine = "Reference  : " + filepath.Base(ref)
		for _, tab := range noteTabs {
			if nm := notes[tab]; nm != nil {
				refLine += fmt.Sprintf("\n  %s → cột %q", tab, nm.Col)
			}
		}
	}
	lines := []string{
		boldStyle.Render("Xác nhận"),
		"UAT        : " + uat.Name() + sheetSuffix(uat),
		"PROD       : " + prod.Name() + sheetSuffix(prod),
		"Key        : " + strings.Join(o.Keys, ", "),
		fmt.Sprintf("So sánh    : %d cột", len(o.Compare)),
		"Ngày 03/04 : hiểu là " + order,
		refLine,
		"Kết quả    : " + out,
	}
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
}

func sheetSuffix(t *Table) string {
	if t.Sheet == "" {
		return ""
	}
	return " (sheet " + t.Sheet + ")"
}

func showResult(res *Result, notes map[string]*NoteMap, out string, d time.Duration) {
	lines := []string{okStyle.Render("✓ So sánh xong") + dimStyle.Render(fmt.Sprintf("  (%.1fs)", d.Seconds()))}
	for _, kv := range res.summaryRows() {
		lines = append(lines, fmt.Sprintf("%s: %s", kv[0], kv[1]))
	}
	for _, tab := range noteTabs {
		if nm := notes[tab]; nm != nil {
			lines = append(lines, fmt.Sprintf("Note tab %s: lấy từ reference, %d key", tab, len(nm.Values)))
		}
	}
	lines = append(lines, boldStyle.Render("File: ")+out)
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
}
