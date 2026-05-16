package content

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func EnsureDirs(notesDir, journalDir string) error {
	if err := os.MkdirAll(notesDir, 0o700); err != nil {
		return err
	}
	return os.MkdirAll(journalDir, 0o700)
}

func AddNote(notesDir, text string) error {
	path := filepath.Join(notesDir, "inbox.md")
	return appendLine(path, "- ["+time.Now().Format("2006-01-02 15:04")+"] "+text)
}

func AddJournalEntry(journalDir, text string) error {
	name := time.Now().Format("2006-01-02") + ".md"
	path := filepath.Join(journalDir, name)
	return appendLine(path, "- ["+time.Now().Format("15:04")+"] "+text)
}

func NoteLines(notesDir string) []string {
	path := filepath.Join(notesDir, "inbox.md")
	return readLastLinesOrHint(path, "Press 'a' to add note")
}

func JournalLines(journalDir string) []string {
	name := time.Now().Format("2006-01-02") + ".md"
	path := filepath.Join(journalDir, name)
	return readLastLinesOrHint(path, "Press 'a' to add journal entry")
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

func readLastLinesOrHint(path, hint string) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{hint}
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return []string{hint}
	}
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return lines
}

func RecentJournalFiles(journalDir string) []string {
	if journalDir == "" {
		return []string{}
	}

	info, err := os.Stat(journalDir)
	if err != nil || !info.IsDir() {
		return []string{}
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo

	_ = filepath.WalkDir(journalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".markdown") {
			return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		files = append(files, fileInfo{path: path, modTime: st.ModTime()})
		return nil
	})

	if len(files) == 0 {
		matches, _ := filepath.Glob(filepath.Join(journalDir, "*.md"))
		for _, match := range matches {
			st, err := os.Stat(match)
			if err == nil {
				files = append(files, fileInfo{path: match, modTime: st.ModTime()})
			}
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	if len(files) > 30 {
		files = files[:30]
	}

	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.path)
	}
	return out
}
