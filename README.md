# lifeops

Tabbed terminal UI for personal operations: machine stats, weather, todos, markdown journal/notes, GitHub, and more.

![Home tab](assets/home-tab.png)
![Home tab](assets/gh-tab.png)

## Install

```bash
go install github.com/sherqo/lifeops@latest
```

Or clone and run:

```bash
git clone https://github.com/sherqo/lifeops && cd lifeops
go run .
```

## Keys

| Key           | Action                           |
| ------------- | -------------------------------- |
| `h` / `l`     | Previous/next tab                |
| `j` / `k`     | Move down/up in lists            |
| `r`           | Refresh current tab              |
| `q`           | Quit                             |
| `a`           | Add item (Todos, Journal, Notes) |
| `e`           | Edit selected Journal/Notes file |
| `w`           | Edit current week journal file   |
| `n` / `p`     | Next/previous month (Calendar)   |
| `T`           | Today in Calendar                |
| `x` / `space` | Toggle todo or habit             |
| `f`           | Cycle todo filter                |
| `[` / `]`     | Switch GitHub section            |
| `Enter`       | Open selected item / GitHub URL  |

## Data

- Config: `~/.config/lifeops/config.json`
- Data: `~/.config/lifeops/lifeops.json`
- Notes (default): `~/.config/lifeops/notes/`
- Journal (default): `~/.config/lifeops/journal/`

Override with env vars `LIFEOPS_NOTES_DIR` and `LIFEOPS_JOURNAL_DIR`, or set `notes_dir` / `journal_dir` in the config file.

## Requirements

- `gh` CLI (authenticated) for the GitHub tab.
- ICS calendar feeds configured in `config.json` for the Calendar tab.
