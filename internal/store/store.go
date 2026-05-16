package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const dbFile = "lifeops.json"

type Todo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Priority  string    `json:"priority"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

type Note struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type JournalEntry struct {
	ID        string    `json:"id"`
	Date      string    `json:"date"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type DB struct {
	Todos   []Todo         `json:"todos"`
	Notes   []Note         `json:"notes"`
	Journal []JournalEntry `json:"journal"`
}

func Load(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, dbFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			db := &DB{}
			if migrated := migrateLegacyFiles(dataDir, db); migrated {
				if err := save(path, db); err != nil {
					return nil, err
				}
			}
			return db, nil
		}
		return nil, err
	}

	var db DB
	if err := json.Unmarshal(raw, &db); err != nil {
		return nil, err
	}
	return &db, nil
}

func Save(dataDir string, db *DB) error {
	path := filepath.Join(dataDir, dbFile)
	return save(path, db)
}

func save(path string, db *DB) error {
	raw, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func AddTodo(db *DB, text string) {
	db.Todos = append(db.Todos, Todo{
		ID:        id("todo"),
		Text:      text,
		Priority:  "normal",
		CreatedAt: time.Now().UTC(),
	})
}

func AddNote(db *DB, text string) {
	db.Notes = append(db.Notes, Note{
		ID:        id("note"),
		Text:      text,
		CreatedAt: time.Now().UTC(),
	})
}

func AddJournalEntry(db *DB, text string) {
	now := time.Now()
	db.Journal = append(db.Journal, JournalEntry{
		ID:        id("jrnl"),
		Date:      now.Format("2006-01-02"),
		Text:      text,
		CreatedAt: now.UTC(),
	})
}

func TodoLines(db *DB) []string {
	if len(db.Todos) == 0 {
		return []string{"Press 'a' to add todo"}
	}
	lines := make([]string, 0, len(db.Todos))
	for _, t := range db.Todos {
		state := "[ ]"
		if t.Completed {
			state = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s %s (%s, %s)", state, t.Text, t.Priority, t.CreatedAt.Local().Format("2006-01-02 15:04")))
	}
	return lines
}

func NoteLines(db *DB) []string {
	if len(db.Notes) == 0 {
		return []string{"Press 'a' to add note"}
	}
	lines := make([]string, 0, len(db.Notes))
	for _, n := range db.Notes {
		lines = append(lines, fmt.Sprintf("- %s (%s)", n.Text, n.CreatedAt.Local().Format("2006-01-02 15:04")))
	}
	return lines
}

func JournalLines(db *DB) []string {
	today := time.Now().Format("2006-01-02")
	var lines []string
	for _, e := range db.Journal {
		if e.Date == today {
			lines = append(lines, fmt.Sprintf("- %s (%s)", e.Text, e.CreatedAt.Local().Format("15:04")))
		}
	}
	if len(lines) == 0 {
		return []string{"Press 'a' to add journal entry"}
	}
	return lines
}

func id(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func migrateLegacyFiles(dataDir string, db *DB) bool {
	migrated := false
	if migrateSimple(filepath.Join(dataDir, "todos.txt"), func(line string) { AddTodo(db, line) }) {
		migrated = true
	}
	if migrateSimple(filepath.Join(dataDir, "notes.txt"), func(line string) { AddNote(db, line) }) {
		migrated = true
	}

	matches, _ := filepath.Glob(filepath.Join(dataDir, "journal-*.txt"))
	sort.Strings(matches)
	for _, path := range matches {
		date := strings.TrimPrefix(filepath.Base(path), "journal-")
		date = strings.TrimSuffix(date, ".txt")
		if migrateSimple(path, func(line string) {
			db.Journal = append(db.Journal, JournalEntry{
				ID:        id("jrnl"),
				Date:      date,
				Text:      line,
				CreatedAt: time.Now().UTC(),
			})
		}) {
			migrated = true
		}
	}
	return migrated
}

func migrateSimple(path string, add func(string)) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	has := false
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		has = true
		add(line)
	}
	if has {
		_ = os.Rename(path, path+".migrated")
	}
	return has
}
