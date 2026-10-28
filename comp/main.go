package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

const version = "1.0.2"

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5A4FCF")).Padding(0, 2)
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5A4FCF")).Padding(0, 1)
	warnStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#E5A50A")).Padding(0, 1)
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#2EA043")).Bold(true)
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149")).Bold(true)
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B949E"))
	boldStyle = lipgloss.NewStyle().Bold(true)
	theme     = huh.ThemeCharm()
)

var errAbort = errors.New("aborted")

// box renders a bordered panel that wraps instead of overflowing the
// terminal, so long messages stay inside the frame.
func box(st lipgloss.Style, s string) string {
	w, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		w = 100
	}
	if lipgloss.Width(s)+4 > w {
		st = st.Width(w - 2)
	}
	return st.Render(s)
}

func main() {
	fmt.Println()
	fmt.Println(titleStyle.Render("comp - So sánh dữ liệu UAT vs PROD  v" + version))
	fmt.Println(dimStyle.Render("Dùng ↑/↓ để chọn, Space để tick, Enter để xác nhận, / để tìm, Ctrl+C để thoát."))
	fmt.Println()

	args := os.Args[1:]
	for {
		err := session(args)
		args = nil
		if errors.Is(err, errAbort) || errors.Is(err, huh.ErrUserAborted) {
			fmt.Println(dimStyle.Render("Đã thoát."))
			return
		}
		if err != nil {
			fmt.Println(errStyle.Render("Lỗi: ") + err.Error())
		}
		var again bool
		if ask(huh.NewConfirm().Title("So sánh cặp file khác?").
			Affirmative("Có").Negative("Thoát").Value(&again)) != nil || !again {
			return
		}
		fmt.Println()
	}
}

func ask(fields ...huh.Field) error {
	err := huh.NewForm(huh.NewGroup(fields...)).WithTheme(theme).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return errAbort
	}
	return err
}

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
		if uatPath, err = pickFile("Chọn file UAT (.csv)", []string{".csv"}, "", "uat"); err != nil {
			return err
		}
		if prodPath, err = pickFile("Chọn file PROD (.csv)", []string{".csv"}, uatPath, "prod"); err != nil {
			return err
		}
	}

	uat, err := ReadCSV(uatPath)
	if err != nil {
		return err
	}
	prod, err := ReadCSV(prodPath)
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

	// Key selection, repeated while the user wants to fix duplicates.
	var nonKeys []string
	for {
		if opts.Keys, err = pickKeys(cc.Common, opts.Keys); err != nil {
			return err
		}
		nonKeys = without(cc.Common, opts.Keys)
		retry, err := checkDuplicates(uat, prod, opts, nonKeys)
		if err != nil {
			return err
		}
		if !retry {
			break
		}
	}

	opts.Compare = nonKeys
	if len(nonKeys) > 0 {
		if err := ask(huh.NewMultiSelect[string]().
			Title("Các cột sẽ được so sánh").
			Description("Mặc định so sánh tất cả. Bỏ tick cột muốn bỏ qua (vd: updated_at).").
			Options(huh.NewOptions(nonKeys...)...).
			Value(&opts.Compare).Filterable(true).Height(min(len(nonKeys)+4, 16))); err != nil {
			return err
		}
		opts.Compare = keepOrder(nonKeys, opts.Compare)
	}

	refPath, ref, err := pickRef(opts.Keys, opts.Order)
	if err != nil {
		return err
	}
	if err := pickSort(&opts); err != nil {
		return err
	}
	outPath, format, err := pickOutput(uat, prod)
	if err != nil {
		return err
	}

	showPlan(uat, prod, opts, refPath, outPath, format)
	start := true
	if err := ask(huh.NewConfirm().Title("Bắt đầu so sánh?").Affirmative("Bắt đầu").Negative("Huỷ").Value(&start)); err != nil {
		return err
	}
	if !start {
		return fmt.Errorf("đã huỷ")
	}

	begin := time.Now()
	res := Compare(uat, prod, opts)
	if ref != nil {
		res.ApplyNotes(ref)
	}
	res.SortRows()

	var written []string
	if format == "xlsx" || format == "both" {
		p := withExt(outPath, ".xlsx")
		if err := res.WriteXLSX(p); err != nil {
			fmt.Println(errStyle.Render("Không ghi được file Excel: ") + err.Error())
			if format == "xlsx" {
				fmt.Println(dimStyle.Render("Chuyển sang ghi CSV."))
				format = "csv"
			}
		} else {
			written = append(written, p)
		}
	}
	if format == "csv" || format == "both" {
		p := withExt(outPath, ".csv")
		if err := res.WriteCSV(p); err != nil {
			return err
		}
		written = append(written, p)
	}
	showResult(res, refPath, written, time.Since(begin))

	if len(written) > 0 && runtime.GOOS == "windows" {
		var open bool
		if ask(huh.NewConfirm().Title("Mở file kết quả?").Affirmative("Mở").Negative("Không").Value(&open)) == nil && open {
			exec.Command("cmd", "/c", "start", "", written[0]).Start()
		}
	}
	return nil
}

// pickFile offers recent files from the working folder, typing or
// dragging a path into the terminal, or browsing folders.
// hint moves the cursor to the first file whose name contains it.
func pickFile(title string, exts []string, near, hint string) (string, error) {
	const typed, browse = "\x00typed", "\x00browse"
	dirs := []string{"."}
	if near != "" {
		dirs = append([]string{filepath.Dir(near)}, dirs...)
	}
	var opts []huh.Option[string]
	seen := map[string]bool{}
	for _, f := range listFiles(dirs, exts) {
		if abs, _ := filepath.Abs(f); !seen[abs] && abs != near {
			seen[abs] = true
			opts = append(opts, huh.NewOption(filepath.Base(f)+dimStyle.Render("  "+filepath.Dir(abs)), abs))
		}
	}
	opts = append(opts,
		huh.NewOption("✎ Nhập hoặc kéo thả đường dẫn file vào đây", typed),
		huh.NewOption("▸ Duyệt thư mục...", browse))
	choice := opts[0].Value
	for _, o := range opts {
		if strings.Contains(strings.ToLower(filepath.Base(o.Value)), hint) {
			choice = o.Value
			break
		}
	}
	if len(opts) > 2 {
		if err := ask(huh.NewSelect[string]().Title(title).Options(opts...).Value(&choice).Height(min(len(opts)+3, 16))); err != nil {
			return "", err
		}
	}
	switch choice {
	case typed:
		var p string
		err := ask(huh.NewInput().Title(title).
			Description("Kéo file từ Explorer thả vào cửa sổ này, hoặc dán đường dẫn, rồi Enter.").
			Value(&p).Validate(func(s string) error {
			s = CleanPath(s)
			if s == "" {
				return errors.New("chưa nhập đường dẫn")
			}
			if st, err := os.Stat(s); err != nil || st.IsDir() {
				return errors.New("không tìm thấy file")
			}
			return nil
		}))
		return CleanPath(p), err
	case browse:
		var p string
		dir, _ := os.Getwd()
		err := ask(huh.NewFilePicker().Title(title).
			Description("Enter để mở thư mục / chọn file, ← hoặc Backspace để lùi ra.").
			CurrentDirectory(dir).AllowedTypes(exts).Picking(true).Height(15).Value(&p))
		return p, err
	}
	return choice, nil
}

func listFiles(dirs, exts []string) []string {
	type fi struct {
		path string
		mod  time.Time
	}
	var all []fi
	for _, d := range dirs {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			for _, x := range exts {
				if ext == x {
					info, err := e.Info()
					if err == nil {
						all = append(all, fi{filepath.Join(d, e.Name()), info.ModTime()})
					}
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.After(all[j].mod) })
	var out []string
	for i, f := range all {
		if i >= 12 {
			break
		}
		out = append(out, f.path)
	}
	return out
}

func showFiles(uat, prod *Table, cc ColumnCheck) {
	info := fmt.Sprintf("%s  %s\n%s  %s\n%s  %d cột",
		boldStyle.Render("UAT :"), fmt.Sprintf("%s  (%d dòng, %d cột)", uat.Name(), len(uat.Rows), len(uat.Headers)),
		boldStyle.Render("PROD:"), fmt.Sprintf("%s  (%d dòng, %d cột)", prod.Name(), len(prod.Rows), len(prod.Headers)),
		boldStyle.Render("Cột chung:"), len(cc.Common))
	info += "\n" + dimStyle.Render(wrapList(cc.Common, 90))
	fmt.Println(box(boxStyle, info))
	if len(cc.OnlyUAT)+len(cc.OnlyProd) > 0 {
		var b strings.Builder
		b.WriteString(boldStyle.Render("⚠ Có cột không trùng tên giữa 2 file. Các cột này sẽ KHÔNG được dùng để so sánh hoặc làm key:"))
		if len(cc.OnlyUAT) > 0 {
			b.WriteString("\n  Chỉ có ở UAT : " + strings.Join(cc.OnlyUAT, ", "))
		}
		if len(cc.OnlyProd) > 0 {
			b.WriteString("\n  Chỉ có ở PROD: " + strings.Join(cc.OnlyProd, ", "))
		}
		fmt.Println(box(warnStyle, b.String()))
	}
	fmt.Println()
}

func wrapList(items []string, width int) string {
	var lines []string
	cur := ""
	for _, it := range items {
		if cur != "" && len(cur)+len(it)+2 > width {
			lines = append(lines, cur+",")
			cur = ""
		}
		if cur != "" {
			cur += ", "
		}
		cur += it
	}
	return strings.Join(append(lines, cur), "\n")
}

func hasAmbiguousDates(uat, prod *Table, cols []string) bool {
	for _, t := range []*Table{uat, prod} {
		ci := colIndex(t)
		for _, c := range cols {
			for i, r := range t.Rows {
				if i > 500 {
					break
				}
				if LooksAmbiguousDate(r[ci[c]]) {
					return true
				}
			}
		}
	}
	return false
}

func pickKeys(common, prev []string) ([]string, error) {
	keys := append([]string{}, prev...)
	if err := ask(huh.NewMultiSelect[string]().
		Title("Chọn cột làm KEY so sánh (1 hoặc nhiều cột)").
		Description("Space để tick, / để tìm theo tên, Enter để xác nhận.").
		Options(huh.NewOptions(common...)...).Value(&keys).Filterable(true).
		Height(min(len(common)+4, 16)).
		Validate(func(v []string) error {
			if len(v) == 0 {
				return errors.New("chọn ít nhất 1 cột key")
			}
			return nil
		})); err != nil {
		return nil, err
	}
	keys = keepOrder(common, keys)
	if len(keys) < 2 {
		return keys, nil
	}
	keep := true
	if err := ask(huh.NewConfirm().
		Title("Thứ tự cột key: " + strings.Join(keys, " → ")).
		Description("Thứ tự này dùng cho các cột đầu của file kết quả.").
		Affirmative("Giữ nguyên").Negative("Sắp xếp lại").Value(&keep)); err != nil {
		return nil, err
	}
	if keep {
		return keys, nil
	}
	var ordered []string
	left := keys
	for len(left) > 1 {
		var k string
		if err := ask(huh.NewSelect[string]().
			Title(fmt.Sprintf("Cột key thứ %d", len(ordered)+1)).
			Options(huh.NewOptions(left...)...).Value(&k)); err != nil {
			return nil, err
		}
		ordered = append(ordered, k)
		left = without(left, []string{k})
	}
	return append(ordered, left...), nil
}

// checkDuplicates warns about keys repeated inside a file. It returns
// true when the user wants to pick the keys again.
func checkDuplicates(uat, prod *Table, opts Options, nonKeys []string) (bool, error) {
	var reps []DupReport
	for _, t := range []*Table{uat, prod} {
		if r := FindDuplicates(t, opts.Keys, nonKeys, opts.Order); r.DupKeys > 0 {
			reps = append(reps, r)
		}
	}
	if len(reps) == 0 {
		fmt.Println(okStyle.Render("✓ Key không bị trùng trong từng file."))
		fmt.Println()
		return false, nil
	}
	var b strings.Builder
	b.WriteString(boldStyle.Render("⚠ Key bị trùng trong cùng 1 file"))
	suggest := map[string]int{}
	for _, r := range reps {
		fmt.Fprintf(&b, "\n%s: %d key trùng (%d dòng). Ví dụ:", r.File, r.DupKeys, r.DupRows)
		for i, e := range r.Examples {
			if i >= 3 {
				break
			}
			b.WriteString("\n  • " + e)
		}
		for _, c := range r.SuggestedCols() {
			suggest[c] += r.DiffCols[c]
		}
	}
	if len(suggest) > 0 {
		var cols []string
		for c := range suggest {
			cols = append(cols, c)
		}
		sort.Slice(cols, func(i, j int) bool { return suggest[cols[i]] > suggest[cols[j]] })
		var parts []string
		for i, c := range cols {
			if i >= 8 {
				parts = append(parts, "...")
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%d)", c, suggest[c]))
		}
		b.WriteString("\nCác dòng trùng key khác nhau ở cột: " + strings.Join(parts, ", "))
		b.WriteString("\nNên chọn thêm ít nhất 1 cột trong số này làm key.")
	} else {
		b.WriteString("\nCác dòng trùng key giống hệt nhau ở mọi cột (dòng bị lặp).")
	}
	fmt.Println(box(warnStyle, b.String()))
	retry := true
	err := ask(huh.NewConfirm().Title("Bạn muốn làm gì?").
		Description("Nếu tiếp tục, các dòng trùng key sẽ được ghép theo thứ tự xuất hiện trong file.").
		Affirmative("Chọn lại key").Negative("Vẫn tiếp tục").Value(&retry))
	fmt.Println()
	return retry, err
}

// pickRef asks for a reference file and checks it against the chosen keys
// right away, so a file that cannot be matched is caught before the run.
func pickRef(keys []string, order DateOrder) (string, *RefNotes, error) {
	use := false
	if err := ask(huh.NewConfirm().Title("Dùng file reference để lấy note?").
		Description("File kết quả cũ (.xlsx hoặc .csv) có cột key và cột note.\nNote chỉ được lấy cho dòng có khác biệt hoặc chỉ có ở 1 bên.").
		Affirmative("Có").Negative("Không").Value(&use)); err != nil || !use {
		return "", nil, err
	}
	for {
		p, err := pickFile("Chọn file reference (.xlsx / .csv)", []string{".xlsx", ".csv"}, "", "sosanh")
		if err != nil {
			return "", nil, err
		}
		rn, err := LoadRefNotes(p, keys, order)
		if err == nil {
			var b strings.Builder
			matched := make([]string, len(rn.KeyPos))
			for i, kp := range rn.KeyPos {
				matched[i] = keys[kp]
			}
			fmt.Fprintf(&b, "Reference: %s (%d note)\nKhớp note theo cột: %s",
				filepath.Base(p), len(rn.Notes), strings.Join(matched, ", "))
			if len(rn.Missing) == 0 {
				fmt.Println(box(boxStyle, okStyle.Render("✓ ")+b.String()))
				fmt.Println()
				return p, rn, nil
			}
			fmt.Fprintf(&b, "\n%s File reference không có cột key: %s\nNote sẽ được khớp chỉ theo các cột ở trên, nhiều dòng có thể nhận cùng 1 note.",
				boldStyle.Render("⚠"), strings.Join(rn.Missing, ", "))
			fmt.Println(box(warnStyle, b.String()))
			keep := true
			if err := ask(huh.NewConfirm().Title("Dùng file reference này?").
				Affirmative("Vẫn dùng").Negative("Chọn file khác").Value(&keep)); err != nil {
				return "", nil, err
			}
			fmt.Println()
			if keep {
				return p, rn, nil
			}
			continue
		}
		fmt.Println(box(warnStyle, boldStyle.Render("⚠ Không dùng được file reference: ")+err.Error()))
		retry := true
		if err := ask(huh.NewConfirm().Title("Chọn file reference khác?").
			Affirmative("Chọn file khác").Negative("Bỏ qua reference").Value(&retry)); err != nil {
			return "", nil, err
		}
		fmt.Println()
		if !retry {
			return "", nil, nil
		}
	}
}

var envOrders = [][3]int{
	{EnvOnlyProd, EnvOnlyUAT, EnvBoth},
	{EnvOnlyUAT, EnvOnlyProd, EnvBoth},
	{EnvBoth, EnvOnlyProd, EnvOnlyUAT},
	{EnvBoth, EnvOnlyUAT, EnvOnlyProd},
	{EnvOnlyProd, EnvBoth, EnvOnlyUAT},
	{EnvOnlyUAT, EnvBoth, EnvOnlyProd},
}

func envOrderLabel(o [3]int) string {
	return EnvLabel[o[0]] + " → " + EnvLabel[o[1]] + " → " + EnvLabel[o[2]]
}

func sortLabel(opts Options) string {
	var parts []string
	for _, s := range opts.Sort {
		switch s.Field {
		case "environment":
			parts = append(parts, "environment ("+envOrderLabel(opts.EnvOrder)+")")
		default:
			dir := "A-Z"
			if s.Desc {
				dir = "Z-A"
			}
			name := s.Field
			if name == "khac_biet" {
				name = colDiff
			}
			parts = append(parts, name+" ("+dir+")")
		}
	}
	return strings.Join(parts, "\n  ")
}

func pickSort(opts *Options) error {
	opts.EnvOrder = envOrders[0]
	opts.Sort = []SortSpec{{Field: "environment"}, {Field: "note", Desc: true}}
	useDefault := true
	if err := ask(huh.NewConfirm().Title("Thứ tự sắp xếp file kết quả").
		Description("Mặc định:\n  " + sortLabel(*opts) + "\nDòng không có note luôn nằm cuối.").
		Affirmative("Dùng mặc định").Negative("Tuỳ chỉnh").Value(&useDefault)); err != nil {
		return err
	}
	if useDefault {
		return nil
	}
	var envOpts []huh.Option[int]
	for i, o := range envOrders {
		envOpts = append(envOpts, huh.NewOption(envOrderLabel(o), i))
	}
	ei := 0
	noteDir := "desc"
	if err := ask(
		huh.NewSelect[int]().Title("Thứ tự cột environment").Options(envOpts...).Value(&ei),
		huh.NewSelect[string]().Title("Sắp xếp cột note").Options(
			huh.NewOption("Z-A (mặc định)", "desc"), huh.NewOption("A-Z", "asc"),
			huh.NewOption("Không sắp xếp theo note", "none")).Value(&noteDir),
	); err != nil {
		return err
	}
	opts.EnvOrder = envOrders[ei]
	opts.Sort = []SortSpec{{Field: "environment"}}
	if noteDir != "none" {
		opts.Sort = append(opts.Sort, SortSpec{Field: "note", Desc: noteDir == "desc"})
	}
	fields := append(append([]string{}, opts.Keys...), opts.Compare...)
	fields = append(fields, "khac_biet")
	for {
		const done = "\x00done"
		var choices []huh.Option[string]
		choices = append(choices, huh.NewOption("✓ Xong", done))
		for _, f := range fields {
			label := f
			if f == "khac_biet" {
				label = colDiff
			}
			choices = append(choices, huh.NewOption(label, f))
		}
		f := done
		if err := ask(huh.NewSelect[string]().Title("Thêm cột để sắp xếp tiếp?").
			Description("Hiện tại:\n  " + sortLabel(*opts)).
			Options(choices...).Value(&f).Height(min(len(choices)+2, 14))); err != nil {
			return err
		}
		if f == done {
			return nil
		}
		desc := false
		if err := ask(huh.NewSelect[bool]().Title("Chiều sắp xếp cho "+f).
			Options(huh.NewOption("A-Z / nhỏ → lớn", false), huh.NewOption("Z-A / lớn → nhỏ", true)).Value(&desc)); err != nil {
			return err
		}
		opts.Sort = append(opts.Sort, SortSpec{Field: f, Desc: desc})
		fields = without(fields, []string{f})
	}
}

func pickOutput(uat, prod *Table) (string, string, error) {
	format := "xlsx"
	base := func(t *Table) string { return strings.TrimSuffix(t.Name(), filepath.Ext(t.Name())) }
	out := filepath.Join(filepath.Dir(uat.Path),
		fmt.Sprintf("SoSanh_%s_vs_%s_%s", base(uat), base(prod), time.Now().Format("20060102_150405")))
	err := ask(
		huh.NewSelect[string]().Title("Định dạng file kết quả").Options(
			huh.NewOption("Excel .xlsx (có tô màu, freeze, filter)", "xlsx"),
			huh.NewOption("CSV (mở bằng Excel vẫn giữ dạng chuỗi)", "csv"),
			huh.NewOption("Cả hai", "both")).Value(&format),
		huh.NewInput().Title("Lưu file kết quả tại (không cần đuôi file)").Value(&out),
	)
	return CleanPath(out), format, err
}

func showPlan(uat, prod *Table, o Options, ref, out, format string) {
	order := "dd/mm/yyyy"
	if o.Order == MDY {
		order = "mm/dd/yyyy"
	}
	if ref == "" {
		ref = "(không dùng)"
	}
	lines := []string{
		boldStyle.Render("Xác nhận"),
		"UAT        : " + uat.Name(),
		"PROD       : " + prod.Name(),
		"Key        : " + strings.Join(o.Keys, ", "),
		fmt.Sprintf("So sánh    : %d cột", len(o.Compare)),
		"Ngày 03/04 : hiểu là " + order,
		"Reference  : " + ref,
		"Sắp xếp    : " + strings.ReplaceAll(sortLabel(o), "\n  ", "\n             "),
		"Kết quả    : " + out + " (" + format + ")",
	}
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
}

func showResult(res *Result, ref string, files []string, d time.Duration) {
	lines := []string{okStyle.Render("✓ So sánh xong") + dimStyle.Render(fmt.Sprintf("  (%.1fs)", d.Seconds()))}
	lines = append(lines,
		boldStyle.Render("Key: ")+strings.Join(res.Opts.Keys, ", "),
		fmt.Sprintf("So sánh %d cột", len(res.Opts.Compare)))
	lines = append(lines, res.SummaryLines()[1:4]...)
	if ref != "" {
		lines = append(lines, fmt.Sprintf("Note lấy từ file reference (%s): %d", filepath.Base(ref), res.NotesFilled))
	}
	for _, f := range files {
		lines = append(lines, boldStyle.Render("File: ")+f)
	}
	fmt.Println(box(boxStyle, strings.Join(lines, "\n")))
}

func without(list, remove []string) []string {
	rm := map[string]bool{}
	for _, r := range remove {
		rm[r] = true
	}
	var out []string
	for _, l := range list {
		if !rm[l] {
			out = append(out, l)
		}
	}
	return out
}

// keepOrder returns the chosen items in the order they appear in ref.
func keepOrder(ref, chosen []string) []string {
	in := map[string]bool{}
	for _, c := range chosen {
		in[c] = true
	}
	var out []string
	for _, r := range ref {
		if in[r] {
			out = append(out, r)
		}
	}
	return out
}

func withExt(p, ext string) string {
	e := strings.ToLower(filepath.Ext(p))
	if e == ".xlsx" || e == ".csv" {
		p = strings.TrimSuffix(p, filepath.Ext(p))
	}
	return p + ext
}
