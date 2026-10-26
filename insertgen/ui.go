package main

// Terminal helpers, same as in uatdiff: styles, prompts and the file
// picker (recent files, drag-and-drop path, or folder browser).

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

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

func ask(fields ...huh.Field) error {
	err := huh.NewForm(huh.NewGroup(fields...)).WithTheme(theme).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return errAbort
	}
	return err
}

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
