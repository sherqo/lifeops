package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	dashboard []string
	weather   []string
	todos     []string
	journal   []string
	notes     []string
	github    []string
	asu       []string
}

type refreshMsg struct{}
type loadedMsg struct {
	tab   string
	lines []string
}

func Run() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(dir, "lifeops")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}

	in := textinput.New()
	in.Placeholder = "Type and press Enter"
	in.CharLimit = 500
	in.Prompt = "> "

	m := model{dataDir: dataDir, input: in, status: "q quit | h/l tabs | r refresh | a add"}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), loadAll(m.dataDir))
}

func tick() tea.Cmd {
	return tea.Tick(2*time.Minute, func(time.Time) tea.Msg { return refreshMsg{} })
}

func loadAll(dataDir string) tea.Cmd {
	return tea.Batch(
		loadDashboard(),
		loadWeather(),
		loadTodos(dataDir),
		loadJournal(dataDir),
		loadNotes(dataDir),
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
				if tabs[m.tab] == "Todos" {
					_ = appendLine(filepath.Join(m.dataDir, "todos.txt"), text)
					m.status = "todo added"
					return m, loadTodos(m.dataDir)
				}
				if tabs[m.tab] == "Notes" {
					_ = appendLine(filepath.Join(m.dataDir, "notes.txt"), text)
					m.status = "note added"
					return m, loadNotes(m.dataDir)
				}
				if tabs[m.tab] == "Journal" {
					path := filepath.Join(m.dataDir, "journal-"+time.Now().Format("2006-01-02")+".txt")
					_ = appendLine(path, time.Now().Format("15:04")+" - "+text)
					m.status = "journal entry added"
					return m, loadJournal(m.dataDir)
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
			m.tab = int(t.String()[0]-'1')
		case "r":
			m.status = "refreshing"
			return m, loadAll(m.dataDir)
		case "a":
			if tabs[m.tab] == "Todos" || tabs[m.tab] == "Notes" || tabs[m.tab] == "Journal" {
				m.inputMode = true
				m.input.Focus()
				m.status = "enter text (esc to cancel)"
			}
		}
	case refreshMsg:
		return m, tea.Batch(loadDashboard(), loadWeather(), loadGitHub(), loadASU(), tick())
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
		case "GitHub":
			m.github = t.lines
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
		return m.github
	default:
		return m.asu
	}
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

func loadTodos(dir string) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Todos", lines: listFileOrHint(filepath.Join(dir, "todos.txt"), "Press 'a' to add todo")} }
}
func loadNotes(dir string) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Notes", lines: listFileOrHint(filepath.Join(dir, "notes.txt"), "Press 'a' to add note")} }
}
func loadJournal(dir string) tea.Cmd {
	return func() tea.Msg {
		path := filepath.Join(dir, "journal-"+time.Now().Format("2006-01-02")+".txt")
		return loadedMsg{tab: "Journal", lines: listFileOrHint(path, "Press 'a' to add journal entry")}
	}
}

func loadGitHub() tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("gh"); err != nil {
			return loadedMsg{tab: "GitHub", lines: []string{"Install GitHub CLI to enable this tab"}}
		}
		auth := run("gh", "auth", "status")
		prs := run("gh", "pr", "list", "-L", "5")
		return loadedMsg{tab: "GitHub", lines: []string{"GitHub snapshot", "", "Auth:", auth, "", "Open PRs:", prs}}
	}
}

func loadASU() tea.Cmd {
	return func() tea.Msg {
		bin := "/home/sherqo/ac/go/eng-asu/asu"
		if _, err := os.Stat(bin); err != nil {
			return loadedMsg{tab: "ASU", lines: []string{"ASU binary not found", "Expected at " + bin}}
		}
		who := run(bin, "whoami", "--json")
		courses := run(bin, "courses", "--json")
		return loadedMsg{tab: "ASU", lines: []string{"ASU data", "", "Whoami:", who, "", "Courses:", courses}}
	}
}

func run(cmd string, args ...string) string {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)) + "\n(err: " + err.Error() + ")"
	}
	text := strings.TrimSpace(string(out))
	if len(text) > 600 {
		text = text[:600] + "..."
	}
	return text
}

func listFileOrHint(path, hint string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{hint}
	}
	var out []string
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line != "" {
			out = append(out, "- "+line)
		}
	}
	if len(out) == 0 {
		return []string{hint}
	}
	return out
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
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
