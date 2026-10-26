package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The DDL library keeps one .sql file per table next to insertgen.exe, so a
// table's DDL only has to be given once.

const libFolder = "ddl_library"

// appDir is where insertgen.exe lives, falling back to the user config
// folder when that is not writable.
func appDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		probe := filepath.Join(dir, ".insertgen_write_test")
		if f, err := os.Create(probe); err == nil {
			f.Close()
			os.Remove(probe)
			return dir
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "insertgen")
	}
	return "."
}

func libDir() string { return filepath.Join(appDir(), libFolder) }

var reUnsafe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func libPath(t *TableDef) string {
	return filepath.Join(libDir(), reUnsafe.ReplaceAllString(t.FullName(), "_")+".sql")
}

// SaveDDL stores the table's DDL. It reports whether a different DDL for
// the same table was replaced.
func SaveDDL(t *TableDef) (path string, replaced bool, err error) {
	path = libPath(t)
	if old, err := os.ReadFile(path); err == nil {
		if strings.TrimSpace(string(old)) == t.SQL {
			return path, false, nil
		}
		replaced = true
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	return path, replaced, os.WriteFile(path, []byte(t.SQL+"\n"), 0o644)
}

type LibEntry struct {
	Path string
	Name string
	Mod  time.Time
}

// ListDDL returns saved tables, most recently used first.
func ListDDL() []LibEntry {
	entries, _ := os.ReadDir(libDir())
	var out []LibEntry
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".sql") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, LibEntry{Path: filepath.Join(libDir(), e.Name()),
			Name: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), Mod: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	return out
}

func LoadDDL(path string) (*TableDef, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tables, err := ParseDDL(strings.TrimPrefix(string(b), "\xef\xbb\xbf"))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	os.Chtimes(path, now, now) // keeps the list ordered by last use
	return tables[0], nil
}

// Settings remembers the user's last choices between runs.
type Settings struct {
	Suffix string `json:"suffix"`
	Digits int    `json:"digits"`
}

func settingsPath() string { return filepath.Join(appDir(), "insertgen_settings.json") }

func LoadSettings() Settings {
	s := Settings{Suffix: "ANHTM", Digits: 2}
	if b, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(b, &s)
	}
	if s.Digits < 1 {
		s.Digits = 2
	}
	return s
}

func SaveSettings(s Settings) {
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		os.WriteFile(settingsPath(), b, 0o644)
	}
}
