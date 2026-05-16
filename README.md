# lifeops

`lifeops` is a tabbed terminal UI for personal operations: machine stats, weather, todos, journal, notes, GitHub, and ASU quick views.

## Features

- Tabs with Vim-like navigation (`h`/`l`, `1-7`, `q`)
- Dashboard with local machine information
- Weather via wttr.in
- Local text-backed todos, notes, and daily journal
- GitHub snapshot using `gh` CLI
- ASU integration by reading the `eng-asu` binary output

## Run

```bash
go run ./
```

## Keys

- `h` / `l`: previous/next tab
- `1..7`: jump to tab
- `a`: add item (Todos, Journal, Notes)
- `?`: toggle keymap help
- `j` / `k`: move GitHub PR selection
- `o` or `Enter`: open selected GitHub PR
- `t`: create todo from selected GitHub PR
- `r`: refresh remote/system tabs
- `q` or `:q`: quit

## Data files

App data is stored in `~/.config/lifeops/lifeops.json`.

Legacy text files (`todos.txt`, `notes.txt`, `journal-*.txt`) are automatically migrated on first run and renamed with `.migrated`.

## Notes

- GitHub tab requires authenticated `gh` CLI.
- ASU tab defaults to `/home/sherqo/ac/go/eng-asu/asu` and can be overridden with `LIFEOPS_ASU_BIN`.
