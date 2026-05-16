# lifeops

`lifeops` is a tabbed terminal UI for personal operations: machine stats, weather, todos, markdown journal/notes, GitHub, ASU quick views, and habits.

## Features

- Tabs with Vim-like navigation (`h`/`l`, `q`)
- Dashboard with local machine information
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
- `:`: command palette (`q`, `refresh`, `tab <name>`)
- `a`: add item (Todos, Journal, Notes)
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

## Notes

- GitHub tab requires authenticated `gh` CLI.
- ASU tab defaults to `/home/sherqo/ac/go/eng-asu/asu` and can be overridden with `LIFEOPS_ASU_BIN`.
