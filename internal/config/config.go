package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const fileName = "config.json"

type Config struct {
	NotesDir   string `json:"notes_dir"`
	JournalDir string `json:"journal_dir"`
}

func Load(dataDir string) (*Config, error) {
	path := filepath.Join(dataDir, fileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ResolvePaths(dataDir string, cfg *Config) (notesDir string, journalDir string) {
	notesDir = cfg.NotesDir
	journalDir = cfg.JournalDir
	if notesDir == "" {
		notesDir = filepath.Join(dataDir, "notes")
	}
	if journalDir == "" {
		journalDir = filepath.Join(dataDir, "journal")
	}
	if v := os.Getenv("LIFEOPS_NOTES_DIR"); v != "" {
		notesDir = v
	}
	if v := os.Getenv("LIFEOPS_JOURNAL_DIR"); v != "" {
		journalDir = v
	}
	return notesDir, journalDir
}
