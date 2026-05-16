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
	"github.com/sherqo/lifeops/internal/store"
)

var tabs = []string{"Dashboard", "Weather", "Todos", "Journal", "Notes", "GitHub", "ASU"}

type model struct {
	tab       int
	width     int
	height    int
	status    string
	helpMode  bool
	inputMode bool
	input     textinput.Model
	dataDir   string
	db        *store.DB

	dashboard []string
	weather   []string
	todos     []string
	journal   []string
	notes     []string
	githubPRs []ghPR
	ghCursor  int
	asu       []string
}

type refreshMsg struct{}
type loadedMsg struct {
	tab   string
	lines []string
}

type githubLoadedMsg struct {
	prs []ghPR
}

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
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

	in := textinput.New()
	in.Placeholder = "Type and press Enter"
	in.CharLimit = 500
	in.Prompt = "> "

	m := model{dataDir: dataDir, db: db, input: in, status: "q quit | h/l tabs | r refresh | a add"}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), loadAll(m.db))
}

func tick() tea.Cmd {
	return tea.Tick(2*time.Minute, func(time.Time) tea.Msg { return refreshMsg{} })
}

func loadAll(db *store.DB) tea.Cmd {
	return tea.Batch(
		loadDashboard(),
		loadWeather(),
		loadTodos(db),
		loadJournal(db),
		loadNotes(db),
		loadGitHub(),
		loadASU(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.inputMode {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		switch t := msg.(type) {
		case tea.KeyMsg:
			switch t.String() {
			case "esc":
				m.inputMode = false
				m.input.Blur()
				m.status = "cancelled"
				return m, nil
			case "enter":
				text := strings.TrimSpace(m.input.Value())
				m.input.Reset()
				m.inputMode = false
				m.input.Blur()
				if text == "" {
					m.status = "empty input"
					return m, nil
				}
				switch tabs[m.tab] {
				case "Todos":
					store.AddTodo(m.db, text)
					_ = store.Save(m.dataDir, m.db)
					m.status = "todo added"
					return m, loadTodos(m.db)
				case "Notes":
					store.AddNote(m.db, text)
					_ = store.Save(m.dataDir, m.db)
					m.status = "note added"
					return m, loadNotes(m.db)
				case "Journal":
					store.AddJournalEntry(m.db, text)
					_ = store.Save(m.dataDir, m.db)
					m.status = "journal entry added"
					return m, loadJournal(m.db)
				}
			}
		}
		return m, cmd
	}

	switch t := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = t.Width, t.Height
	case tea.KeyMsg:
		switch t.String() {
		case "q", "ctrl+c", ":q":
			return m, tea.Quit
		case "?":
			m.helpMode = !m.helpMode
			if m.helpMode {
				m.status = "help open"
			} else {
				m.status = "help closed"
			}
		case "h":
			if m.tab > 0 {
				m.tab--
			}
		case "l":
			if m.tab < len(tabs)-1 {
				m.tab++
			}
		case "1", "2", "3", "4", "5", "6", "7":
			m.tab = int(t.String()[0] - '1')
		case "r":
			m.status = "refreshing"
			return m, loadAll(m.db)
		case "a":
			if tabs[m.tab] == "Todos" || tabs[m.tab] == "Notes" || tabs[m.tab] == "Journal" {
				m.inputMode = true
				m.input.Focus()
				m.status = "enter text (esc to cancel)"
			}
		case "j":
			if tabs[m.tab] == "GitHub" && m.ghCursor < len(m.githubPRs)-1 {
				m.ghCursor++
				m.status = "selected PR moved down"
			}
		case "k":
			if tabs[m.tab] == "GitHub" && m.ghCursor > 0 {
				m.ghCursor--
				m.status = "selected PR moved up"
			}
		case "o", "enter":
			if tabs[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				go exec.Command("gh", "pr", "view", fmt.Sprintf("%d", pr.Number), "--repo", pr.Repo.NameWithOwner, "--web").Run()
				m.status = "opened PR in browser"
			}
		case "t":
			if tabs[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				store.AddTodo(m.db, fmt.Sprintf("Review PR #%d: %s", pr.Number, pr.Title))
				_ = store.Save(m.dataDir, m.db)
				m.status = "todo created from PR"
				return m, loadTodos(m.db)
			}
		}
	case refreshMsg:
		return m, tea.Batch(loadDashboard(), loadWeather(), loadGitHub(), loadASU(), tick())
	case githubLoadedMsg:
		m.githubPRs = t.prs
		if m.ghCursor >= len(m.githubPRs) {
			m.ghCursor = max(0, len(m.githubPRs)-1)
		}
		m.status = "updated GitHub"
	case loadedMsg:
		switch t.tab {
		case "Dashboard":
			m.dashboard = t.lines
		case "Weather":
			m.weather = t.lines
		case "Todos":
			m.todos = t.lines
		case "Journal":
			m.journal = t.lines
		case "Notes":
			m.notes = t.lines
		case "ASU":
			m.asu = t.lines
		}
		m.status = "updated " + t.tab
	}

	return m, nil
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

	body := strings.Join(currentTab(m), "\n")
	if m.helpMode {
		body = strings.Join([]string{
			"Keymap",
			"",
			"h/l ........ switch tabs",
			"1..7 ....... jump to tab",
			"a .......... add item in Todos/Journal/Notes",
			"j/k ........ move GitHub PR selection",
			"o or Enter . open selected GitHub PR",
			"t .......... create todo from selected PR",
			"r .......... refresh system/network tabs",
			"? .......... toggle help",
			"q or :q .... quit",
		}, "\n")
	}
	if m.inputMode {
		body += "\n\n" + m.input.View()
	}

	return strings.Join([]string{
		strings.Join(head, "  "),
		strings.Repeat("-", max(20, m.width-2)),
		body,
		"",
		"Status: " + m.status,
	}, "\n")
}

func currentTab(m model) []string {
	switch tabs[m.tab] {
	case "Dashboard":
		return m.dashboard
	case "Weather":
		return m.weather
	case "Todos":
		return m.todos
	case "Journal":
		return m.journal
	case "Notes":
		return m.notes
	case "GitHub":
		return renderGitHub(m.githubPRs, m.ghCursor)
	default:
		return m.asu
	}
}

func renderGitHub(prs []ghPR, cursor int) []string {
	lines := []string{"GitHub snapshot", "", "Pull requests:", "Use j/k to select, o/Enter to open, t to add todo", ""}
	if len(prs) == 0 {
		return append(lines, "No open pull requests or gh not authenticated")
	}
	for i, pr := range prs {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s#%d %s (%s)", prefix, pr.Number, pr.Title, pr.Repo.NameWithOwner))
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
				TempC      string `json:"temp_C"`
				FeelsLikeC string `json:"FeelsLikeC"`
				Humidity   string `json:"humidity"`
				Desc       []struct {
					Value string `json:"value"`
				} `json:"weatherDesc"`
			} `json:"current_condition"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Current) == 0 {
			return loadedMsg{tab: "Weather", lines: []string{"Could not decode weather data"}}
		}
		c := payload.Current[0]
		desc := ""
		if len(c.Desc) > 0 {
			desc = c.Desc[0].Value
		}
		return loadedMsg{tab: "Weather", lines: []string{"Current weather", "", "Temp: " + c.TempC + "C", "Feels: " + c.FeelsLikeC + "C", "Humidity: " + c.Humidity + "%", "Condition: " + desc}}
	}
}

func loadTodos(db *store.DB) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Todos", lines: store.TodoLines(db)} }
}

func loadNotes(db *store.DB) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Notes", lines: store.NoteLines(db)} }
}

func loadJournal(db *store.DB) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Journal", lines: store.JournalLines(db)} }
}

func loadGitHub() tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("gh"); err != nil {
			return githubLoadedMsg{}
		}
		raw := run("gh", "pr", "list", "-L", "10", "--json", "number,title,url,repository")
		var prs []ghPR
		if err := json.Unmarshal([]byte(raw), &prs); err != nil {
			return githubLoadedMsg{}
		}
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
		courses := run(bin, "courses", "--json")
		return loadedMsg{tab: "ASU", lines: []string{"ASU data", "", "Whoami:", who, "", "Courses:", courses}}
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
	text := strings.TrimSpace(string(out))
	if len(text) > 600 {
		text = text[:600] + "..."
	}
	return text
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
	used := (total - avail) / 1024 / 1024
	tot := total / 1024 / 1024
	return [2]float64{used, tot}, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
