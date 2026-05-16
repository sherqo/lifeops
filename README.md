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
- `r`: refresh remote/system tabs
- `q` or `:q`: quit

## Data files

App data is stored in `~/.config/lifeops`:

- `todos.txt`
- `notes.txt`
- `journal-YYYY-MM-DD.txt`

## Notes

- GitHub tab requires authenticated `gh` CLI.
- ASU tab expects binary at `/home/sherqo/ac/go/eng-asu/asu`.
