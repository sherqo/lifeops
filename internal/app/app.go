package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sherqo/lifeops/internal/config"
	"github.com/sherqo/lifeops/internal/content"
	"github.com/sherqo/lifeops/internal/store"
)

var tabs = []string{"Dashboard", "Weather", "Todos", "Journal", "Notes", "GitHub", "ASU", "Habits"}

type mode int

const (
	modeNormal mode = iota
	modeInput
	modeCommand
)

type model struct {
	tab         int
	width       int
	status      string
	helpMode    bool
	mode        mode
	input       textinput.Model
	command     textinput.Model
	dataDir     string
	notesDir    string
	journalDir  string
	db          *store.DB
	todoCursor  int
	todoFilter  store.TodoFilter
	ghCursor    int
	githubPRs   []ghPR
	dashboard   []string
	weather     []string
	asu         []string
	habitCursor int
}

type refreshMsg struct{}
type loadedMsg struct {
	tab   string
	lines []string
}
type githubLoadedMsg struct{ prs []ghPR }

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Repo   struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func Run() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dataDir := dir + "/lifeops"
	db, err := store.Load(dataDir)
	if err != nil {
		return err
	}
	store.EnsureDefaultHabits(db)
	cfg, err := config.Load(dataDir)
	if err != nil {
		return err
	}
	notesDir, journalDir := config.ResolvePaths(dataDir, cfg)
	if err := content.EnsureDirs(notesDir, journalDir); err != nil {
		return err
	}

	in := textinput.New()
	in.Placeholder = "Type and press Enter"
	in.Prompt = "> "
	cmd := textinput.New()
	cmd.Placeholder = "q | refresh | tab <name>"
	cmd.Prompt = ":"

	m := model{dataDir: dataDir, notesDir: notesDir, journalDir: journalDir, db: db, input: in, command: cmd, status: "q quit | : command | ? help"}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m model) Init() tea.Cmd { return tea.Batch(tick(), loadAll(m.db)) }
func tick() tea.Cmd           { return tea.Tick(2*time.Minute, func(time.Time) tea.Msg { return refreshMsg{} }) }
func loadAll(db *store.DB) tea.Cmd {
	return tea.Batch(loadDashboard(), loadWeather(), loadGitHub(), loadASU(), loadTodos(db))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == modeInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.String() == "esc" {
				m.mode = modeNormal
				m.input.Blur()
				return m, nil
			}
			if k.String() == "enter" {
				text := strings.TrimSpace(m.input.Value())
				m.input.Reset()
				m.mode = modeNormal
				m.input.Blur()
				if text == "" {
					return m, nil
				}
				switch tabs[m.tab] {
				case "Todos":
					store.AddTodo(m.db, text)
					_ = store.Save(m.dataDir, m.db)
					return m, loadTodos(m.db)
				case "Notes":
					_ = content.AddNote(m.notesDir, text)
					return m, nil
				case "Journal":
					_ = content.AddJournalEntry(m.journalDir, text)
					return m, nil
				}
			}
		}
		return m, cmd
	}
	if m.mode == modeCommand {
		var cmd tea.Cmd
		m.command, cmd = m.command.Update(msg)
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.String() == "esc" {
				m.mode = modeNormal
				m.command.Blur()
				return m, nil
			}
			if k.String() == "enter" {
				c := strings.TrimSpace(m.command.Value())
				m.command.Reset()
				m.mode = modeNormal
				m.command.Blur()
				return m, runCommand(&m, c)
			}
		}
		return m, cmd
	}

	switch t := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = t.Width
	case tea.KeyMsg:
		switch t.String() {
		case "q", "ctrl+c", ":q":
			return m, tea.Quit
		case ":":
			m.mode = modeCommand
			m.command.Focus()
			return m, nil
		case "?":
			m.helpMode = !m.helpMode
			return m, nil
		case "h":
			if m.tab > 0 {
				m.tab--
			}
		case "l":
			if m.tab < len(tabs)-1 {
				m.tab++
			}
		case "r":
			return m, loadAll(m.db)
		case "a":
			if tabs[m.tab] == "Todos" || tabs[m.tab] == "Notes" || tabs[m.tab] == "Journal" {
				m.mode = modeInput
				m.input.Focus()
			}
		case "j":
			m = moveDown(m)
		case "k":
			m = moveUp(m)
		case "x":
			if tabs[m.tab] == "Todos" && store.ToggleVisibleTodo(m.db, m.todoFilter, m.todoCursor) {
				_ = store.Save(m.dataDir, m.db)
			}
		case "f":
			if tabs[m.tab] == "Todos" {
				m.todoFilter = store.NextFilter(m.todoFilter)
				m.todoCursor = 0
			}
		case "o", "enter":
			if tabs[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				go exec.Command("gh", "pr", "view", fmt.Sprintf("%d", pr.Number), "--repo", pr.Repo.NameWithOwner, "--web").Run()
			}
		case "t":
			if tabs[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				store.AddTodo(m.db, fmt.Sprintf("Review PR #%d: %s", pr.Number, pr.Title))
				_ = store.Save(m.dataDir, m.db)
			}
		case " ":
			if tabs[m.tab] == "Habits" && len(m.db.Habits) > 0 {
				i := m.habitCursor
				m.db.Habits[i].Completed = !m.db.Habits[i].Completed
				_ = store.Save(m.dataDir, m.db)
			}
		}
	case refreshMsg:
		return m, tea.Batch(loadDashboard(), loadWeather(), loadGitHub(), loadASU(), tick())
	case loadedMsg:
		if t.tab == "Dashboard" {
			m.dashboard = t.lines
		}
		if t.tab == "Weather" {
			m.weather = t.lines
		}
		if t.tab == "ASU" {
			m.asu = t.lines
		}
	case githubLoadedMsg:
		m.githubPRs = t.prs
	}
	return m, nil
}

func runCommand(m *model, c string) tea.Cmd {
	parts := strings.Fields(c)
	if len(parts) == 0 {
		return nil
	}
	switch parts[0] {
	case "q":
		return tea.Quit
	case "refresh":
		return loadAll(m.db)
	case "tab":
		if len(parts) > 1 {
			name := strings.ToLower(strings.Join(parts[1:], " "))
			for i, t := range tabs {
				if strings.ToLower(t) == name {
					m.tab = i
					break
				}
			}
		}
	}
	return nil
}

func moveDown(m model) model {
	if tabs[m.tab] == "GitHub" && m.ghCursor < len(m.githubPRs)-1 {
		m.ghCursor++
	}
	if tabs[m.tab] == "Todos" {
		n := len(store.VisibleTodoIndices(m.db, m.todoFilter))
		if m.todoCursor < n-1 {
			m.todoCursor++
		}
	}
	if tabs[m.tab] == "Habits" && m.habitCursor < len(m.db.Habits)-1 {
		m.habitCursor++
	}
	return m
}

func moveUp(m model) model {
	if tabs[m.tab] == "GitHub" && m.ghCursor > 0 {
		m.ghCursor--
	}
	if tabs[m.tab] == "Todos" && m.todoCursor > 0 {
		m.todoCursor--
	}
	if tabs[m.tab] == "Habits" && m.habitCursor > 0 {
		m.habitCursor--
	}
	return m
}

func (m model) View() string {
	active := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	inactive := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	var head []string
	for i, t := range tabs {
		if i == m.tab {
			head = append(head, active.Render("["+t+"]"))
		} else {
			head = append(head, inactive.Render(t))
		}
	}
	body := strings.Join(m.currentTab(), "\n")
	if m.helpMode {
		body = "?: help | : command | a add | h/l tabs | j/k move\nTodos: x toggle, f filter\nGitHub: o open, t todo\nHabits: space toggle"
	}
	if m.mode == modeInput {
		body += "\n\n" + m.input.View()
	}
	if m.mode == modeCommand {
		body += "\n\n" + m.command.View()
	}
	return strings.Join([]string{strings.Join(head, "  "), strings.Repeat("-", max(20, m.width-2)), body, "", "Status: " + m.status}, "\n")
}

func (m model) currentTab() []string {
	switch tabs[m.tab] {
	case "Dashboard":
		return m.dashboard
	case "Weather":
		return m.weather
	case "Todos":
		return renderTodos(m.db, m.todoFilter, m.todoCursor)
	case "Journal":
		return content.JournalLines(m.journalDir)
	case "Notes":
		return content.NoteLines(m.notesDir)
	case "GitHub":
		return renderGitHub(m.githubPRs, m.ghCursor)
	case "ASU":
		return m.asu
	default:
		return renderHabits(m.db.Habits, m.habitCursor)
	}
}

func renderTodos(db *store.DB, filter store.TodoFilter, cursor int) []string {
	idx := store.VisibleTodoIndices(db, filter)
	lines := []string{"Todos (filter: " + store.FilterLabel(filter) + ")", "j/k move, x toggle, f cycle filter", ""}
	if len(idx) == 0 {
		return append(lines, "No todos")
	}
	for i, j := range idx {
		t := db.Todos[j]
		mark, p := "[ ]", "  "
		if t.Completed {
			mark = "[x]"
		}
		if i == cursor {
			p = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", p, mark, t.Text))
	}
	return lines
}

func renderHabits(h []store.Habit, cursor int) []string {
	lines := []string{"Habits", "j/k move, space toggle", ""}
	for i, x := range h {
		m, p := "[ ]", "  "
		if x.Completed {
			m = "[x]"
		}
		if i == cursor {
			p = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", p, m, x.Name))
	}
	return lines
}

func renderGitHub(prs []ghPR, cursor int) []string {
	lines := []string{"GitHub", "j/k select, o open, t make todo", ""}
	if len(prs) == 0 {
		return append(lines, "No open pull requests")
	}
	for i, pr := range prs {
		p := "  "
		if i == cursor {
			p = "> "
		}
		lines = append(lines, fmt.Sprintf("%s#%d %s (%s)", p, pr.Number, pr.Title, pr.Repo.NameWithOwner))
	}
	return lines
}

func loadDashboard() tea.Cmd {
	return func() tea.Msg {
		host, _ := os.Hostname()
		lines := []string{"Machine info", "", "Host: " + host, "OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH}
		if b, err := os.ReadFile("/proc/loadavg"); err == nil {
			lines = append(lines, "Load: "+strings.TrimSpace(string(b)))
		}
		if mem, err := parseMem(); err == nil {
			lines = append(lines, fmt.Sprintf("Memory: %.1f/%.1f GiB", mem[0], mem[1]))
		}
		return loadedMsg{tab: "Dashboard", lines: lines}
	}
}

func loadWeather() tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequest(http.MethodGet, "https://wttr.in/?format=j1", nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return loadedMsg{tab: "Weather", lines: []string{"Weather unavailable", err.Error()}}
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var payload struct {
			Current []struct {
				TempC, FeelsLikeC, Humidity string
				Desc                        []struct{ Value string } `json:"weatherDesc"`
			} `json:"current_condition"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Current) == 0 {
			return loadedMsg{tab: "Weather", lines: []string{"Could not decode weather data"}}
		}
		c := payload.Current[0]
		d := ""
		if len(c.Desc) > 0 {
			d = c.Desc[0].Value
		}
		return loadedMsg{tab: "Weather", lines: []string{"Current weather", "", "Temp: " + c.TempC + "C", "Feels: " + c.FeelsLikeC + "C", "Humidity: " + c.Humidity + "%", "Condition: " + d}}
	}
}

func loadTodos(db *store.DB) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Todos", lines: renderTodos(db, store.FilterAll, 0)} }
}

func loadGitHub() tea.Cmd {
	return func() tea.Msg {
		raw := run("gh", "pr", "list", "-L", "10", "--json", "number,title,repository")
		var prs []ghPR
		_ = json.Unmarshal([]byte(raw), &prs)
		return githubLoadedMsg{prs: prs}
	}
}

func loadASU() tea.Cmd {
	return func() tea.Msg {
		bin := asuBinaryPath()
		if _, err := os.Stat(bin); err != nil {
			return loadedMsg{tab: "ASU", lines: []string{"ASU binary not found", "Expected at " + bin}}
		}
		who := run(bin, "whoami", "--json")
		return loadedMsg{tab: "ASU", lines: []string{"ASU data", "", "Whoami:", who}}
	}
}

func asuBinaryPath() string {
	if v := strings.TrimSpace(os.Getenv("LIFEOPS_ASU_BIN")); v != "" {
		return v
	}
	return "/home/sherqo/ac/go/eng-asu/asu"
}
func run(cmd string, args ...string) string {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(string(out))
}

func parseMem() ([2]float64, error) {
	var total, avail float64
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return [2]float64{}, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[0] == "MemTotal:" {
			fmt.Sscanf(f[1], "%f", &total)
		}
		if f[0] == "MemAvailable:" {
			fmt.Sscanf(f[1], "%f", &avail)
		}
	}
	return [2]float64{(total - avail) / 1024 / 1024, total / 1024 / 1024}, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
