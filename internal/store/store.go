package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const dbFile = "lifeops.json"

type TodoFilter int

const (
	FilterAll TodoFilter = iota
	FilterOpen
	FilterDone
)

type Todo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Priority  string    `json:"priority"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

type DB struct {
	Todos  []Todo  `json:"todos"`
	Habits []Habit `json:"habits"`
	UI     UIState `json:"ui"`
}

type Habit struct {
	Name      string `json:"name"`
	Completed bool   `json:"completed"`
}

type UIState struct {
	Expanded map[string]bool `json:"expanded"`
}

func Load(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, dbFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DB{}, nil
		}
		return nil, err
	}

	var db DB
	if err := json.Unmarshal(raw, &db); err != nil {
		return nil, err
	}
	if db.UI.Expanded == nil {
		db.UI.Expanded = map[string]bool{}
	}
	return &db, nil
}

func Save(dataDir string, db *DB) error {
	raw, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, dbFile), raw, 0o600)
}

func AddTodo(db *DB, text string) {
	db.Todos = append(db.Todos, Todo{
		ID:        fmt.Sprintf("todo_%d", time.Now().UnixNano()),
		Text:      text,
		Priority:  "normal",
		CreatedAt: time.Now().UTC(),
	})
}

func VisibleTodoIndices(db *DB, filter TodoFilter) []int {
	out := make([]int, 0, len(db.Todos))
	for i, t := range db.Todos {
		if filter == FilterOpen && t.Completed {
			continue
		}
		if filter == FilterDone && !t.Completed {
			continue
		}
		out = append(out, i)
	}
	return out
}

func ToggleVisibleTodo(db *DB, filter TodoFilter, visibleIndex int) bool {
	idx := VisibleTodoIndices(db, filter)
	if visibleIndex < 0 || visibleIndex >= len(idx) {
		return false
	}
	actual := idx[visibleIndex]
	db.Todos[actual].Completed = !db.Todos[actual].Completed
	return true
}

func FilterLabel(f TodoFilter) string {
	switch f {
	case FilterOpen:
		return "open"
	case FilterDone:
		return "done"
	default:
		return "all"
	}
}

func NextFilter(f TodoFilter) TodoFilter {
	return (f + 1) % 3
}

func EnsureDefaultHabits(db *DB) {
	if len(db.Habits) > 0 {
		return
	}
	db.Habits = []Habit{{Name: "Hydrate"}, {Name: "Read 20 minutes"}, {Name: "Workout"}}
}

func IsExpanded(db *DB, key string) bool {
	if db == nil {
		return false
	}
	if db.UI.Expanded == nil {
		return false
	}
	return db.UI.Expanded[key]
}

func SetExpanded(db *DB, key string, expanded bool) {
	if db == nil {
		return
	}
	if db.UI.Expanded == nil {
		db.UI.Expanded = map[string]bool{}
	}
	if expanded {
		db.UI.Expanded[key] = true
		return
	}
	delete(db.UI.Expanded, key)
}
