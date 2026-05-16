# lifeops

`lifeops` is a tabbed terminal UI for personal operations: machine stats, weather, todos, markdown journal/notes, GitHub, ASU quick views, and habits.

## Features

- Tabs with Vim-like navigation (`h`/`l`, `q`)
- Dashboard with local machine information
- Calendar tab with month view and Google Calendar events
- Weather via wttr.in
- JSON-backed todos with toggles and filters
- Markdown-backed notes and daily journal
- GitHub snapshot using `gh` CLI
- ASU integration by reading the `eng-asu` binary output
- Habit tracker

## Run

```bash
go run ./
```

## Keys

- `h` / `l`: previous/next tab
- `:`: command palette (`q`, `refresh`, `tab <name>`, `set-journal <path>`, `set-notes <path>`, `show-paths`)
- `n` / `p`: next/previous month in Calendar tab
- `T`: jump Calendar tab to current month
- `a`: add item (Todos, Journal, Notes)
- `e`: open selected Journal file or Notes inbox in `$EDITOR` (falls back to `nvim`/`vi`)
- `Enter` on Journal: open selected `.md` journal file
- `?`: toggle keymap help
- `j` / `k`: move selection in lists (Todos, GitHub, Habits)
- `x`: toggle selected todo complete/incomplete
- `f`: cycle todo filter (all/open/done)
- `o` or `Enter`: open selected GitHub PR
- `t`: create todo from selected GitHub PR
- `space`: toggle selected habit
- `r`: refresh remote/system tabs
- `q` or `:q`: quit

## Data files

Todos and habits are stored in `~/.config/lifeops/lifeops.json`.

Notes and journal are markdown files, with configurable locations:
- `LIFEOPS_NOTES_DIR` (default: `~/.config/lifeops/notes`)
- `LIFEOPS_JOURNAL_DIR` (default: `~/.config/lifeops/journal`)

Journal tab is file-oriented: it lists recent `*.md` entries from your configured journal path and opens them directly in your editor.

You can also set paths in `~/.config/lifeops/config.json` (see `config.example.json`). Env vars take precedence.

## Notes

- GitHub tab requires authenticated `gh` CLI.
- Calendar uses configured ICS feeds from `~/.config/lifeops/config.json` (`calendar_ics_urls`).
- Keep private ICS links in local config only; do not commit them.
- GitHub tab uses `gh search prs --author @me --state open` so it works across repositories.
- ASU tab defaults to `/home/sherqo/ac/go/eng-asu/asu` and can be overridden with `LIFEOPS_ASU_BIN`.
- Weather defaults to Cairo (`wttr.in/Cairo`) and shows detected place.
