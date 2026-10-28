package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

const version = "2.0.0"

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
