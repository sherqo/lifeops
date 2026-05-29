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

func EnsureDirs(notesDir, journalDir, booksDir string) error {
	if err := os.MkdirAll(notesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return err
	}
	return os.MkdirAll(booksDir, 0o700)
}

func AddNote(notesDir, text string) error {
	// Normalize the text to be a valid filename (lowercase, dashes instead of spaces)
	name := normalizeFilename(text) + ".md"
	path := filepath.Join(notesDir, name)
	// Create empty file - user will fill it in editor
	return os.WriteFile(path, []byte(""), 0o600)
}

func AddJournalEntry(journalDir, text string) error {
	// Normalize the text to be a valid filename (lowercase, dashes instead of spaces)
	name := normalizeFilename(text) + ".md"
	path := filepath.Join(journalDir, name)
	// Create empty file - user will fill it in editor
	return os.WriteFile(path, []byte(""), 0o600)
}

func normalizeFilename(text string) string {
	// Convert to lowercase and replace spaces with dashes
	result := strings.ToLower(text)
	result = strings.ReplaceAll(result, " ", "-")
	// Remove any characters that aren't alphanumeric or dashes
	var cleaned []rune
	for _, r := range result {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			cleaned = append(cleaned, r)
		}
	}
	return string(cleaned)
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
		// Accept both old format (2026-05-16.md) and new format (2026-05-16_1530.md)
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".markdown") {
			return nil
		}
		// Skip the old daily journal file - it's now just historical
		if strings.HasSuffix(name, ".md") && !strings.Contains(name, "_") {
			// Could still include if you want, but let's focus on new entries
			// return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		files = append(files, fileInfo{path: path, modTime: st.ModTime()})
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		wi, oki := weekNumberFromFileName(filepath.Base(files[i].path))
		wj, okj := weekNumberFromFileName(filepath.Base(files[j].path))
		if oki && okj {
			if wi != wj {
				return wi > wj
			}
			return files[i].modTime.After(files[j].modTime)
		}
		if oki != okj {
			return oki
		}
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

func weekNumberFromFileName(name string) (int, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	lower = strings.TrimSuffix(lower, filepath.Ext(lower))
	if !strings.HasPrefix(lower, "week-") {
		return 0, false
	}
	rest := lower[len("week-"):]
	if rest == "" {
		return 0, false
	}
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}

func RecentNotesFiles(notesDir string) []string {
	if notesDir == "" {
		return []string{}
	}

	info, err := os.Stat(notesDir)
	if err != nil || !info.IsDir() {
		return []string{}
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo

	_ = filepath.WalkDir(notesDir, func(path string, d os.DirEntry, walkErr error) error {
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

func RecentBookFiles(booksDir string) []string {
	if booksDir == "" {
		return []string{}
	}

	info, err := os.Stat(booksDir)
	if err != nil || !info.IsDir() {
		return []string{}
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo

	_ = filepath.WalkDir(booksDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".pdf") && !strings.HasSuffix(name, ".epub") && !strings.HasSuffix(name, ".mobi") {
			return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		files = append(files, fileInfo{path: path, modTime: st.ModTime()})
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	if len(files) > 60 {
		files = files[:60]
	}

	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.path)
	}
	return out
}
