package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
)

// The output tabs that can take notes from a reference file.
var noteTabs = []string{sheetRecords, sheetFields}

func session(args []string) error {
	var uatPath, prodPath string
	var err error
	// "uatprod" is the classic flow. "other" (e.g. two analysis files) lets
	// the user choose which row statuses each reference file fills in.
	mode := "uatprod"
	if err := ask(huh.NewSelect[string]().
		Title("Bạn đang so sánh loại dữ liệu nào?").
		Options(huh.NewOption("Dữ liệu UAT vs PROD", "uatprod"),
			huh.NewOption("Khác (vd: 2 file phân tích khác biệt)", "other")).
		Value(&mode)); err != nil {
		return err
	}
	if len(args) >= 2 {
		uatPath, prodPath = CleanPath(args[0]), CleanPath(args[1])
		correct := true
		if err := ask(huh.NewConfirm().
			Title("Đã nhận 2 file").
			Description(fmt.Sprintf("file 1 = %s\nfile 2 = %s", filepath.Base(uatPath), filepath.Base(prodPath))).
			Affirmative("Đúng").Negative("Đảo lại").Value(&correct)); err != nil {
			return err
		}
		if !correct {
			uatPath, prodPath = prodPath, uatPath
		}
	} else {
		if uatPath, err = pickFile("Chọn file 1 (.csv / .xlsx)", []string{".csv", ".xlsx"}, "", "uat", false); err != nil {
			return err
		}
		if prodPath, err = pickFile("Chọn file 2 (.csv / .xlsx)", []string{".csv", ".xlsx"}, uatPath, "prod", false); err != nil {
			return err
		}
	}

	uat, err := loadInput(uatPath, "1")
	if err != nil {
		return err
	}
	prod, err := loadInput(prodPath, "2")
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
		if len(nonKeys) > 0 {
			err := askBack(huh.NewMultiSelect[string]().
				Title("Các cột sẽ được so sánh").
				Description("Mặc định so sánh tất cả. Bỏ tick cột muốn bỏ qua (vd: updated_at).\nEsc: quay lại chọn key.").
				Options(huh.NewOptions(nonKeys...)...).
				Value(&opts.Compare).Filterable(true).Height(min(len(nonKeys)+4, 16)))
			if errors.Is(err, errBack) {
				continue
			}
			if err != nil {
				return err
			}
		}
		opts.Compare = keepOrder(nonKeys, opts.Compare)
		break
	}

	refPath, notes, err := pickRefs(opts.Keys, opts.Order, mode == "other")
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

	if runtime.GOOS == "windows" {
		var open bool
		if ask(huh.NewConfirm().Title("Mở file kết quả?").Affirmative("Mở").Negative("Không").Value(&open)) == nil && open {
			exec.Command("cmd", "/c", "start", "", out).Start()
		}
	}
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
func pickRefs(keys []string, order DateOrder, other bool) (string, map[string][]*NoteMap, error) {
	for {
		p, notes, err := pickRefOnce(keys, order, other)
		if errors.Is(err, errBack) {
			continue
		}
		return p, notes, err
	}
}

// pickRefOnce asks whether to use a reference, then for the file. It
// returns errBack when the user goes back from the file step.
func pickRefOnce(keys []string, order DateOrder, other bool) (string, map[string][]*NoteMap, error) {
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
		notes := map[string][]*NoteMap{}
		var lines []string
		for _, tab := range noteTabs {
			nm, err := pickTabNotes(p, names, tab, keys, order)
			if err != nil {
				lines = append(lines, boldStyle.Render(tab+": ")+err.Error())
				continue
			}
			if nm != nil {
				notes[tab] = []*NoteMap{nm}
				lines = append(lines, fmt.Sprintf("%s: %d note, cột %q → cột %q", tab, len(nm.Values), nm.src, nm.Col))
			}
		}
		refs := []string{filepath.Base(p)}
		if other && len(notes[sheetRecords]) > 0 {
			more, err := moreRecordRefs(notes, keys, order)
			if err != nil {
				return "", nil, err
			}
			refs = append(refs, more...)
			if len(more) > 0 {
				lines = append(lines, fmt.Sprintf("%s: %d file reference", sheetRecords, len(notes[sheetRecords])))
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
		label := strings.Join(refs, ", ")
		fmt.Println(box(boxStyle, okStyle.Render("✓ Reference: ")+label+"\n"+strings.Join(lines, "\n")))
		fmt.Println()
		return label, notes, nil
	}
}

var statusChoices = []struct {
	status int
	label  string
}{
	{StatusOnly1, "Chỉ file 1"},
	{StatusOnly2, "Chỉ file 2"},
	{StatusMatch, "Khớp hoàn toàn ở cả 2 file"},
}

// pickStatuses asks which records rows nm fills in, defaulting to the
// statuses no earlier reference covers (all three for the first one), and
// adds the chosen statuses to covered.
func pickStatuses(nm *NoteMap, covered map[int]bool) error {
	var chosen []int
	var opts []huh.Option[int]
	for _, c := range statusChoices {
		opts = append(opts, huh.NewOption(c.label, c.status))
		if !covered[c.status] {
			chosen = append(chosen, c.status)
		}
	}
	if err := ask(huh.NewMultiSelect[int]().
		Title("File reference này lấy note cho dòng nào của tab records?").
		Description("Space để tick, Enter để xác nhận.").
		Options(opts...).Value(&chosen).
		Validate(func(v []int) error {
			if len(v) == 0 {
				return errors.New("chọn ít nhất 1 loại dòng")
			}
			return nil
		})); err != nil {
		return err
	}
	nm.Apply = map[int]bool{}
	for _, s := range chosen {
		nm.Apply[s] = true
		covered[s] = true
	}
	return nil
}

// moreRecordRefs asks which records rows the first reference fills in and,
// while some statuses stay uncovered, offers to add more reference files for
// the records tab. It appends to notes and returns the extra file names.
func moreRecordRefs(notes map[string][]*NoteMap, keys []string, order DateOrder) ([]string, error) {
	covered := map[int]bool{}
	if err := pickStatuses(notes[sheetRecords][0], covered); err != nil {
		return nil, err
	}
	var files []string
	for len(covered) < len(statusChoices) {
		more := false
		if err := ask(huh.NewConfirm().Title("Chọn thêm file reference khác cho tab records?").
			Description("Một số loại dòng chưa có note từ file reference nào.").
			Affirmative("Có").Negative("Không").Value(&more)); err != nil {
			return nil, err
		}
		if !more {
			break
		}
		p, err := pickFile("Chọn file reference tiếp theo (.xlsx / .csv)", []string{".xlsx", ".csv"}, "", "sosanh", true)
		if errors.Is(err, errBack) {
			continue
		}
		if err != nil {
			return nil, err
		}
		names, err := SheetNames(p)
		if err != nil {
			fmt.Println(box(warnStyle, boldStyle.Render("⚠ Không đọc được file reference: ")+err.Error()))
			continue
		}
		nm, err := pickTabNotes(p, names, sheetRecords, keys, order)
		if err != nil {
			fmt.Println(box(warnStyle, boldStyle.Render("⚠ ")+err.Error()))
			continue
		}
		if nm == nil {
			continue
		}
		if err := pickStatuses(nm, covered); err != nil {
			return nil, err
		}
		notes[sheetRecords] = append(notes[sheetRecords], nm)
		files = append(files, filepath.Base(p))
	}
	return files, nil
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
	if err := ask(huh.NewSelect[string]().Title(fmt.Sprintf("Tab %q: lấy note từ cột nào của sheet %q?", tab, sheet)).
		Options(sourceOpts...).Value(&source)); err != nil {
		return nil, err
	}
	// The output column defaults to the name of the chosen source column.
	target := source
	if err := ask(
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
// file 1.
func pickOutput(uat, prod *Table) (string, error) {
	base := func(t *Table) string { return strings.TrimSuffix(t.Name(), filepath.Ext(t.Name())) }
	out := filepath.Join(filepath.Dir(uat.Path), fmt.Sprintf("comp_%s_vs_%s.xlsx", base(uat), base(prod)))
	if err := ask(huh.NewInput().Title("Lưu file kết quả tại (.xlsx)").Value(&out)); err != nil {
		return "", err
	}
	return withExt(CleanPath(out), ".xlsx"), nil
}

func showPlan(uat, prod *Table, o Options, ref string, notes map[string][]*NoteMap, out string) {
	order := "dd/mm/yyyy"
	if o.Order == MDY {
		order = "mm/dd/yyyy"
	}
	refLine := "Reference  : (không dùng)"
	if ref != "" {
		refLine = "Reference  : " + ref
		for _, tab := range noteTabs {
			if nms := notes[tab]; len(nms) > 0 {
				refLine += fmt.Sprintf("\n  %s → cột %q", tab, nms[0].Col)
			}
		}
	}
	lines := []string{
		boldStyle.Render("Xác nhận"),
		"File 1     : " + uat.Name() + sheetSuffix(uat),
		"File 2     : " + prod.Name() + sheetSuffix(prod),
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

func showResult(res *Result, notes map[string][]*NoteMap, out string, d time.Duration) {
	lines := []string{okStyle.Render("✓ So sánh xong") + dimStyle.Render(fmt.Sprintf("  (%.1fs)", d.Seconds()))}
	for _, kv := range res.summaryRows() {
		lines = append(lines, fmt.Sprintf("%s: %s", kv[0], kv[1]))
	}
	for _, tab := range noteTabs {
		for _, nm := range notes[tab] {
			lines = append(lines, fmt.Sprintf("Note tab %s: lấy từ reference, %d key", tab, len(nm.Values)))
		}
	}
	lines = append(lines, boldStyle.Render("File: ")+out)
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
}
