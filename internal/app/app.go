package app

import (
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
	"github.com/sherqo/lifeops/internal/config"
	"github.com/sherqo/lifeops/internal/content"
	"github.com/sherqo/lifeops/internal/store"
)

// Color styles
var (
	tabActive    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	tabInactive  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selected     = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	normalItem   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	header       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	subtext      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	statusBar    = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("235"))
	divider      = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	doneItem     = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Strikethrough(true)
	errorText    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	successText  = lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
)

var tabs = []string{"Home", "Calendar", "Weather", "Todos", "Journal", "Notes", "GitHub", "ASU", "Habits"}

type mode int

const (
	modeNormal mode = iota
	modeInput
	modeCommand
)

type model struct {
	tab           int
	width         int
	height        int
	status        string
	helpMode      bool
	mode          mode
	input         textinput.Model
	command       textinput.Model
	dataDir       string
	notesDir      string
	journalDir    string
	cfg           *config.Config
	db            *store.DB
	todoCursor    int
	todoFilter    store.TodoFilter
	ghCursor      int
	githubPRs     []ghPR
	githubErr     string
	dashboard     []string
	calendar      []string
	calMonth      time.Time
	calendarICS   []string
	weather       []string
	asu           []string
	habitCursor   int
	journalCursor int
	notesCursor  int
}

type refreshMsg struct{}
type loadedMsg struct {
	tab   string
	lines []string
}
type githubLoadedMsg struct{ prs []ghPR }
type githubErrorMsg struct{ err string }
type journalRefreshMsg struct{}
type dashboardStatsMsg struct {
	openTodos  int
	doneTodos  int
	habitsDone int
	habitsTotal int
}

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Repo   struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

var defaultCalendarICS []string

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
	calendarICS := cfg.CalendarICS
	if len(calendarICS) == 0 {
		calendarICS = defaultCalendarICS
	}

	in := textinput.New()
	in.Placeholder = "Type and press Enter"
	in.Prompt = "> "
	cmd := textinput.New()
	cmd.Placeholder = "q | refresh | tab <name> | set-journal <path>"
	cmd.Prompt = ":"

	m := model{dataDir: dataDir, notesDir: notesDir, journalDir: journalDir, cfg: cfg, db: db, input: in, command: cmd, status: "q quit | : command | ? help", calMonth: firstOfMonth(time.Now()), calendarICS: calendarICS}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m model) Init() tea.Cmd { return tea.Batch(tick(), loadDashboardWithStats(m.db), loadTodos(m.db)) }
func tick() tea.Cmd           { return tea.Tick(2*time.Minute, func(time.Time) tea.Msg { return refreshMsg{} }) }

func loadTab(tab string, db *store.DB, month time.Time, feeds []string) tea.Cmd {
	switch tab {
	case "Dashboard":
		return loadDashboardWithStats(db)
	case "Calendar":
		return loadCalendar(month, feeds)
	case "Weather":
		return loadWeather()
	case "GitHub":
		return loadGitHub()
	case "ASU":
		return loadASU()
	case "Todos":
		return loadTodos(db)
	default:
		return nil
	}
}

func loadAll(db *store.DB, month time.Time, feeds []string) tea.Cmd {
	return tea.Batch(loadDashboard(), loadCalendar(month, feeds), loadWeather(), loadGitHub(), loadASU(), loadTodos(db))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == modeInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.String() == "esc" {
				m.mode = modeNormal
				m.input.Blur()
				m.status = "input cancelled"
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
					m.status = "todo added"
					return m, loadTodos(m.db)
				case "Notes":
					_ = content.AddNote(m.notesDir, text)
					m.status = "note added"
					m.notesCursor = 0
					return m, func() tea.Msg { return journalRefreshMsg{} }
				case "Journal":
					_ = content.AddJournalEntry(m.journalDir, text)
					m.status = "journal entry added"
					m.journalCursor = 0
					return m, func() tea.Msg { return journalRefreshMsg{} }
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
				m.status = "command cancelled"
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
		m.height = t.Height
	case tea.KeyMsg:
		switch t.String() {
		case "q", "ctrl+c", ":q":
			return m, tea.Quit
		case ":":
			m.mode = modeCommand
			m.command.Focus()
			m.status = "command mode"
			return m, nil
		case "?":
			m.helpMode = !m.helpMode
			return m, nil
		case "h":
			if m.tab > 0 {
				m.tab--
				return m, loadTab(tabs[m.tab], m.db, m.calMonth, m.calendarICS)
			}
		case "l":
			if m.tab < len(tabs)-1 {
				m.tab++
				return m, loadTab(tabs[m.tab], m.db, m.calMonth, m.calendarICS)
			}
		case "r":
			m.status = "refreshing..."
			return m, loadTab(tabs[m.tab], m.db, m.calMonth, m.calendarICS)
		case "n":
			if tabs[m.tab] == "Calendar" {
				m.calMonth = m.calMonth.AddDate(0, 1, 0)
				m.status = "calendar next month"
				return m, loadCalendar(m.calMonth, m.calendarICS)
			}
		case "p":
			if tabs[m.tab] == "Calendar" {
				m.calMonth = m.calMonth.AddDate(0, -1, 0)
				m.status = "calendar previous month"
				return m, loadCalendar(m.calMonth, m.calendarICS)
			}
		case "T":
			if tabs[m.tab] == "Calendar" {
				m.calMonth = firstOfMonth(time.Now())
				m.status = "calendar current month"
				return m, loadCalendar(m.calMonth, m.calendarICS)
			}
		case "a":
			if tabs[m.tab] == "Todos" || tabs[m.tab] == "Notes" || tabs[m.tab] == "Journal" {
				m.mode = modeInput
				m.input.Focus()
				m.status = "enter text and press Enter"
			}
		case "e":
			if tabs[m.tab] == "Journal" {
				m.status = "opening journal in editor"
				return m, openInEditorCmd(selectedJournalPath(m.journalDir, m.journalCursor))
			}
			if tabs[m.tab] == "Notes" {
				m.status = "opening notes in editor"
				return m, openInEditorCmd(selectedNotesPath(m.notesDir, m.notesCursor))
			}
		case "j":
			m = moveDown(m)
		case "k":
			m = moveUp(m)
		case "x":
			if tabs[m.tab] == "Todos" && store.ToggleVisibleTodo(m.db, m.todoFilter, m.todoCursor) {
				_ = store.Save(m.dataDir, m.db)
				m.status = "todo toggled"
			}
		case "f":
			if tabs[m.tab] == "Todos" {
				m.todoFilter = store.NextFilter(m.todoFilter)
				m.todoCursor = 0
				m.status = "todo filter: " + store.FilterLabel(m.todoFilter)
			}
		case "o", "enter":
			if tabs[m.tab] == "Journal" {
				m.status = "opening journal in editor"
				return m, openInEditorCmd(selectedJournalPath(m.journalDir, m.journalCursor))
			}
			if tabs[m.tab] == "Notes" {
				m.status = "opening notes in editor"
				return m, openInEditorCmd(selectedNotesPath(m.notesDir, m.notesCursor))
			}
			if tabs[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				if pr.URL != "" {
					m.status = "PR URL: " + pr.URL
				} else {
					m.status = "no PR URL available"
				}
			}
		case "t":
			if tabs[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				store.AddTodo(m.db, fmt.Sprintf("Review PR #%d: %s", pr.Number, pr.Title))
				_ = store.Save(m.dataDir, m.db)
				m.status = "todo created from PR"
			}
		case " ":
			if tabs[m.tab] == "Habits" && len(m.db.Habits) > 0 {
				i := m.habitCursor
				m.db.Habits[i].Completed = !m.db.Habits[i].Completed
				_ = store.Save(m.dataDir, m.db)
				m.status = "habit toggled"
			}
		}
	case refreshMsg:
		return m, tea.Batch(loadDashboard(), loadCalendar(m.calMonth, m.calendarICS), loadWeather(), loadGitHub(), loadASU(), tick())
	case loadedMsg:
		if t.tab == "Dashboard" {
			m.dashboard = t.lines
		}
		if t.tab == "Calendar" {
			m.calendar = t.lines
		}
		if t.tab == "Weather" {
			m.weather = t.lines
		}
		if t.tab == "ASU" {
			m.asu = t.lines
		}
	case githubLoadedMsg:
		m.githubPRs = t.prs
		m.githubErr = ""
	case githubErrorMsg:
		m.githubPRs = nil
		m.githubErr = t.err
	case journalRefreshMsg:
		m.status = "journal refreshed"
	case dashboardStatsMsg:
		m.dashboard = buildFullDashboard(t)
		m.status = "dashboard updated"
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
		return loadAll(m.db, m.calMonth, m.calendarICS)
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
	case "set-journal":
		if len(parts) > 1 {
			path := strings.TrimSpace(strings.Join(parts[1:], " "))
			if path != "" {
				m.journalDir = path
				if m.cfg != nil {
					m.cfg.JournalDir = path
					_ = config.Save(m.dataDir, m.cfg)
				}
				_ = content.EnsureDirs(m.notesDir, m.journalDir)
				m.journalCursor = 0
				m.status = "journal path updated"
			}
		}
	case "set-notes":
		if len(parts) > 1 {
			path := strings.TrimSpace(strings.Join(parts[1:], " "))
			if path != "" {
				m.notesDir = path
				if m.cfg != nil {
					m.cfg.NotesDir = path
					_ = config.Save(m.dataDir, m.cfg)
				}
				_ = content.EnsureDirs(m.notesDir, m.journalDir)
				m.status = "notes path updated"
			}
		}
	case "show-paths":
		m.status = "journal: " + m.journalDir
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
	if tabs[m.tab] == "Journal" {
		n := len(journalFilesForDisplay(m.journalDir))
		if m.journalCursor < n-1 {
			m.journalCursor++
		}
	}
	if tabs[m.tab] == "Notes" {
		n := len(notesFilesForDisplay(m.notesDir))
		if m.notesCursor < n-1 {
			m.notesCursor++
		}
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
	if tabs[m.tab] == "Journal" && m.journalCursor > 0 {
		m.journalCursor--
	}
	if tabs[m.tab] == "Notes" && m.notesCursor > 0 {
		m.notesCursor--
	}
	return m
}

func (m model) View() string {
	var head []string
	for i, t := range tabs {
		if i == m.tab {
			head = append(head, tabActive.Render("["+t+"]"))
		} else {
			head = append(head, tabInactive.Render(t))
		}
	}
	tabBar := strings.Join(head, "  ")
	body := strings.Join(m.currentTab(), "\n")
	if m.helpMode {
		body = subtext.Render("?: help | : command | a add | h/l tabs | j/k move") + "\n" +
			subtext.Render("Calendar: n/p month, T today") + "\n" +
			subtext.Render("Todos: x toggle, f filter") + "\n" +
			subtext.Render("Journal/Notes: e open in editor") + "\n" +
			subtext.Render("GitHub: o show URL, t todo") + "\n" +
			subtext.Render("Habits: space toggle")
	}
	if m.mode == modeInput {
		body += "\n\n" + m.input.View()
	}
	if m.mode == modeCommand {
		body += "\n\n" + m.command.View()
	}
	
	// Controls at bottom - add padding to push to bottom of terminal
	controls := subtext.Render("h/l: tabs | j/k: move | r: refresh | ?: help | q: quit")
	status := statusBar.Render(" " + m.status)
	
	// Calculate padding to push controls to bottom
	bodyLines := strings.Count(body, "\n") + 2
	padding := ""
	if m.height > bodyLines + 2 {
		for i := 0; i < m.height - bodyLines - 2; i++ {
			padding += "\n"
		}
	}
	
	return strings.Join([]string{
		tabBar,
		divider.Render(strings.Repeat("─", max(20, m.width-2))),
		body,
		padding,
		controls,
		status,
	}, "\n")
}

func (m model) currentTab() []string {
	switch tabs[m.tab] {
	case "Home":
		return m.dashboard
	case "Calendar":
		return m.calendar
	case "Weather":
		return m.weather
	case "Todos":
		return renderTodos(m.db, m.todoFilter, m.todoCursor)
	case "Journal":
		return renderJournal(m.journalDir, m.journalCursor)
	case "Notes":
		return renderNotes(m.notesDir, m.notesCursor)
	case "GitHub":
		return renderGitHub(m.githubPRs, m.ghCursor, m.githubErr)
	case "ASU":
		return m.asu
	default:
		return renderHabits(m.db.Habits, m.habitCursor)
	}
}

func renderJournal(journalDir string, cursor int) []string {
	files := journalFilesForDisplay(journalDir)
	lines := []string{}

	if _, err := os.Stat(journalDir); os.IsNotExist(err) {
		lines = append(lines, errorText.Render("ERROR: Journal directory does not exist: "+journalDir))
		lines = append(lines, subtext.Render("Use :set-journal <path> to set a valid path"))
	} else {
		lines = append(lines, subtext.Render("Path: "+journalDir+" | Files: "+fmt.Sprintf("%d", len(files))))
	}

	lines = append(lines, subtext.Render("j/k select, Enter/e open, a create new"), "")

	if len(files) == 0 {
		return append(lines, subtext.Render("No journal files - press 'a' to create one"))
	}
	for i, path := range files {
		name := filepath.Base(path)
		var itemStyle lipgloss.Style
		if i == cursor {
			itemStyle = selected.Bold(true)
		} else {
			itemStyle = normalItem
		}
		lines = append(lines, itemStyle.Render(name))
	}
	return lines
}

func journalFilesForDisplay(journalDir string) []string {
	files := content.RecentJournalFiles(journalDir)
	today := journalTodayPath(journalDir)
	hasToday := false
	for _, f := range files {
		if f == today {
			hasToday = true
			break
		}
	}
	if !hasToday {
		files = append([]string{today}, files...)
	}
	return files
}

func selectedJournalPath(journalDir string, cursor int) string {
	files := journalFilesForDisplay(journalDir)
	if len(files) == 0 {
		return journalTodayPath(journalDir)
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(files) {
		cursor = len(files) - 1
	}
	return files[cursor]
}

func renderNotes(notesDir string, cursor int) []string {
	files := notesFilesForDisplay(notesDir)
	lines := []string{}

	if _, err := os.Stat(notesDir); os.IsNotExist(err) {
		lines = append(lines, errorText.Render("ERROR: Notes directory does not exist: "+notesDir))
		lines = append(lines, subtext.Render("Use :set-notes <path> to set a valid path"))
	} else {
		lines = append(lines, subtext.Render("Path: "+notesDir+" | Files: "+fmt.Sprintf("%d", len(files))))
	}

	lines = append(lines, subtext.Render("j/k select, Enter/e open, a create new"), "")

	if len(files) == 0 {
		return append(lines, subtext.Render("No notes - press 'a' to create one"))
	}
	for i, path := range files {
		name := filepath.Base(path)
		var itemStyle lipgloss.Style
		if i == cursor {
			itemStyle = selected.Bold(true)
		} else {
			itemStyle = normalItem
		}
		lines = append(lines, itemStyle.Render(name))
	}
	return lines
}

func notesFilesForDisplay(notesDir string) []string {
	files := content.RecentNotesFiles(notesDir)
	if len(files) == 0 {
		return []string{}
	}
	// Sort by modification time, newest first is already done in RecentNotesFiles
	return files
}

func selectedNotesPath(notesDir string, cursor int) string {
	files := notesFilesForDisplay(notesDir)
	if len(files) == 0 {
		return ""
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(files) {
		cursor = len(files) - 1
	}
	return files[cursor]
}

func renderTodos(db *store.DB, filter store.TodoFilter, cursor int) []string {
	idx := store.VisibleTodoIndices(db, filter)
	lines := []string{subtext.Render("filter: "+store.FilterLabel(filter)+" | j/k move, x toggle, f cycle")}
	if len(idx) == 0 {
		return append(lines, subtext.Render("No todos"))
	}
	for i, j := range idx {
		t := db.Todos[j]
		mark := "[ ]"
		var itemStyle lipgloss.Style
		if t.Completed {
			mark = "[x]"
			itemStyle = doneItem
		} else {
			itemStyle = normalItem
		}
		if i == cursor {
			itemStyle = selected.Bold(true)
		}
		lines = append(lines, itemStyle.Render(mark+" "+t.Text))
	}
	return lines
}

func renderHabits(h []store.Habit, cursor int) []string {
	lines := []string{subtext.Render("j/k move, space toggle")}
	for i, x := range h {
		mark := "[ ]"
		var itemStyle lipgloss.Style
		if x.Completed {
			mark = "[x]"
			itemStyle = doneItem
		} else {
			itemStyle = normalItem
		}
		if i == cursor {
			itemStyle = selected.Bold(true)
		}
		lines = append(lines, itemStyle.Render(mark+" "+x.Name))
	}
	return lines
}

func renderGitHub(prs []ghPR, cursor int, errMsg string) []string {
	lines := []string{subtext.Render("j/k select, o show URL, t make todo")}
	if errMsg != "" {
		return append(lines, errorText.Render("Error: "+errMsg))
	}
	if len(prs) == 0 {
		return append(lines, subtext.Render("No open pull requests"))
	}
	for i, pr := range prs {
		var itemStyle lipgloss.Style
		if i == cursor {
			itemStyle = selected.Bold(true)
		} else {
			itemStyle = normalItem
		}
		lines = append(lines, itemStyle.Render(fmt.Sprintf("#%d %s (%s)", pr.Number, pr.Title, pr.Repo.NameWithOwner)))
	}
	return lines
}

func loadDashboardWithStats(db *store.DB) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		host, _ := os.Hostname()
		
		var lines []string
		
		// Header with date/time
		lines = append(lines, subtext.Render(now.Format("Monday, January 2, 2006 • 15:04")))
		lines = append(lines, divider.Render(""))
		
		// Machine section
		lines = append(lines, header.Render("Machine"))
		osInfo := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
		lines = append(lines, normalItem.Render("Host: "+host))
		lines = append(lines, normalItem.Render("OS: "+osInfo))
		
		if b, err := os.ReadFile("/proc/loadavg"); err == nil {
			load := strings.TrimSpace(string(b))
			parts := strings.Fields(load)
			if len(parts) > 0 {
				lines = append(lines, normalItem.Render("Load: "+parts[0]))
			}
		}
		if mem, err := parseMem(); err == nil {
			lines = append(lines, normalItem.Render(fmt.Sprintf("Memory: %.1f/%.1f GiB", mem[0], mem[1])))
		}
		lines = append(lines, divider.Render(""))
		
		// Stats section - compute directly
		lines = append(lines, header.Render("Stats"))
		
		openTodos, doneTodos := 0, 0
		for _, t := range db.Todos {
			if t.Completed {
				doneTodos++
			} else {
				openTodos++
			}
		}
		habitsDone := 0
		for _, h := range db.Habits {
			if h.Completed {
				habitsDone++
			}
		}
		
		todoStr := fmt.Sprintf("Todos: %d open, %d done", openTodos, doneTodos)
		todoColor := successText
		if openTodos > 3 {
			todoColor = errorText
		}
		lines = append(lines, todoColor.Render(todoStr))
		
		habitStr := fmt.Sprintf("Habits: %d/%d done", habitsDone, len(db.Habits))
		habitColor := successText
		if len(db.Habits) > 0 && habitsDone < len(db.Habits) {
			habitColor = subtext
		}
		lines = append(lines, habitColor.Render(habitStr))
		
		return loadedMsg{tab: "Dashboard", lines: lines}
	}
}

func loadDashboard() tea.Cmd {
	return loadDashboardWithStats(nil)
}

func loadDashboardStats(db *store.DB) tea.Cmd {
	return func() tea.Msg {
		open := 0
		done := 0
		for _, t := range db.Todos {
			if t.Completed {
				done++
			} else {
				open++
			}
		}
		habitsDone := 0
		for _, h := range db.Habits {
			if h.Completed {
				habitsDone++
			}
		}
		return dashboardStatsMsg{
			openTodos:  open,
			doneTodos:  done,
			habitsDone: habitsDone,
			habitsTotal: len(db.Habits),
		}
	}
}

func buildFullDashboard(stats dashboardStatsMsg) []string {
	var lines []string
	now := time.Now()
	host, _ := os.Hostname()
	
	lines = append(lines, subtext.Render(now.Format("Monday, January 2, 2006 • 15:04")))
	lines = append(lines, divider.Render(""))
	
	lines = append(lines, header.Render("Machine"))
	osInfo := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	lines = append(lines, normalItem.Render("Host: "+host))
	lines = append(lines, normalItem.Render("OS: "+osInfo))
	
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		load := strings.TrimSpace(string(b))
		parts := strings.Fields(load)
		if len(parts) > 0 {
			lines = append(lines, normalItem.Render("Load: "+parts[0]))
		}
	}
	if mem, err := parseMem(); err == nil {
		lines = append(lines, normalItem.Render(fmt.Sprintf("Memory: %.1f/%.1f GiB", mem[0], mem[1])))
	}
	lines = append(lines, divider.Render(""))
	
	lines = append(lines, header.Render("Stats"))
	todoStr := fmt.Sprintf("Todos: %d open, %d done", stats.openTodos, stats.doneTodos)
	todoColor := successText
	if stats.openTodos > 3 {
		todoColor = errorText
	}
	lines = append(lines, todoColor.Render(todoStr))
	
	habitStr := fmt.Sprintf("Habits: %d/%d done", stats.habitsDone, stats.habitsTotal)
	habitColor := successText
	if stats.habitsTotal > 0 && stats.habitsDone < stats.habitsTotal {
		habitColor = subtext
	}
	lines = append(lines, habitColor.Render(habitStr))
	
	return lines
}

func loadCalendar(month time.Time, feeds []string) tea.Cmd {
	return func() tea.Msg {
		lines := []string{
			subtext.Render("n next month, p previous month, T current month"),
			"",
			header.Render(month.Format("January 2006")),
		}
		lines = append(lines, renderMonth(month)...)
		lines = append(lines, "", subtext.Render("Google Calendar events:"))
		lines = append(lines, loadGoogleEvents(month, feeds)...)
		return loadedMsg{tab: "Calendar", lines: lines}
	}
}

func renderMonth(month time.Time) []string {
	first := firstOfMonth(month)
	last := first.AddDate(0, 1, -1)
	offset := int(first.Weekday())
	if offset == 0 {
		offset = 7
	}
	offset--

	lines := []string{"Mo Tu We Th Fr Sa Su"}
	week := make([]string, 0, 7)
	for i := 0; i < offset; i++ {
		week = append(week, "  ")
	}

	today := time.Now()
	for day := 1; day <= last.Day(); day++ {
		d := time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, first.Location())
		cell := fmt.Sprintf("%2d", day)
		if d.Year() == today.Year() && d.YearDay() == today.YearDay() {
			cell = "[" + fmt.Sprintf("%d", day) + "]"
		}
		week = append(week, cell)
		if len(week) == 7 {
			lines = append(lines, strings.Join(week, " "))
			week = week[:0]
		}
	}
	if len(week) > 0 {
		for len(week) < 7 {
			week = append(week, "  ")
		}
		lines = append(lines, strings.Join(week, " "))
	}
	return lines
}

func loadGoogleEvents(month time.Time, feeds []string) []string {
	if len(feeds) == 0 {
		return []string{"No calendar feeds configured"}
	}
	var lines []string
	for i, feed := range feeds {
		name := fmt.Sprintf("Calendar %d", i+1)
		if strings.Contains(feed, "holiday") {
			name = "Egypt Holidays"
		} else if strings.Contains(feed, "import.calendar.google.com") {
			name = "Imported Calendar"
		} else if strings.Contains(feed, "sharqawycs") {
			name = "Personal Calendar"
		}
		lines = append(lines, name+":")
		items := loadICSEvents(feed, month)
		if len(items) == 0 {
			lines = append(lines, "- No events")
		} else {
			lines = append(lines, items...)
		}
		lines = append(lines, "")
	}
	return lines
}

func loadICSEvents(url string, month time.Time) []string {
	res, err := http.Get(url)
	if err != nil {
		return []string{"ICS fetch failed: " + err.Error()}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return []string{"ICS read failed: " + err.Error()}
	}
	items := parseICSMonth(string(raw), month)
	if len(items) == 0 {
		return nil
	}
	if len(items) > 8 {
		items = append(items[:8], "...")
	}
	return items
}

func parseICSMonth(ics string, month time.Time) []string {
	lines := strings.Split(ics, "\n")
	monthStart := firstOfMonth(month)
	monthEnd := monthStart.AddDate(0, 1, 0)
	var out []string

	inEvent := false
	var dt string
	var summary string

	flush := func() {
		if dt == "" || summary == "" {
			return
		}
		t, err := parseICSTime(dt)
		if err != nil {
			return
		}
		if !t.Before(monthStart) && t.Before(monthEnd) {
			out = append(out, "- "+t.Format("2006-01-02 15:04")+" | "+summary)
		}
	}

	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch line {
		case "BEGIN:VEVENT":
			inEvent = true
			dt = ""
			summary = ""
			continue
		case "END:VEVENT":
			if inEvent {
				flush()
			}
			inEvent = false
			continue
		}
		if !inEvent {
			continue
		}
		if strings.HasPrefix(line, "DTSTART") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				dt = parts[1]
			}
		}
		if strings.HasPrefix(line, "SUMMARY:") {
			summary = strings.TrimPrefix(line, "SUMMARY:")
		}
	}

	return out
}

func parseICSTime(v string) (time.Time, error) {
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		return t.Local(), nil
	}
	if t, err := time.Parse("20060102T150405", v); err == nil {
		return t, nil
	}
	if t, err := time.Parse("20060102", v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("bad ICS time")
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func loadWeather() tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequest(http.MethodGet, "https://wttr.in/Cairo?format=j1", nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return loadedMsg{tab: "Weather", lines: []string{"Weather unavailable", err.Error()}}
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var payload struct {
			NearestArea []struct {
				AreaName []struct {
					Value string `json:"value"`
				} `json:"areaName"`
				Country []struct {
					Value string `json:"value"`
				} `json:"country"`
			} `json:"nearest_area"`
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
		d := ""
		if len(c.Desc) > 0 {
			d = c.Desc[0].Value
		}
		place := "Cairo, Egypt"
		if len(payload.NearestArea) > 0 {
			if len(payload.NearestArea[0].AreaName) > 0 && payload.NearestArea[0].AreaName[0].Value != "" {
				place = payload.NearestArea[0].AreaName[0].Value
			}
			if len(payload.NearestArea[0].Country) > 0 && payload.NearestArea[0].Country[0].Value != "" {
				place += ", " + payload.NearestArea[0].Country[0].Value
			}
		}
		return loadedMsg{tab: "Weather", lines: []string{subtext.Render("Place: " + place), "Temp: " + c.TempC + "C", "Feels: " + c.FeelsLikeC + "C", "Humidity: " + c.Humidity + "%", "Condition: " + d}}
	}
}

func loadTodos(db *store.DB) tea.Cmd {
	return func() tea.Msg { return loadedMsg{tab: "Todos", lines: renderTodos(db, store.FilterAll, 0)} }
}

func loadGitHub() tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("gh"); err != nil {
			return githubErrorMsg{err: "gh CLI not found"}
		}
		if err := exec.Command("gh", "auth", "status").Run(); err != nil {
			return githubErrorMsg{err: "gh not authenticated"}
		}
		raw := run("gh", "search", "prs", "--author", "@me", "--state", "open", "--limit", "20", "--json", "number,title,url,repository")
		var prs []ghPR
		if err := json.Unmarshal([]byte(raw), &prs); err != nil {
			return githubErrorMsg{err: "failed to parse gh output"}
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
		who := run(bin, "whoami")
		courses := run(bin, "courses")
		return loadedMsg{tab: "ASU", lines: []string{"Profile:", trimLong(who, 18), "", "Courses:", trimLong(courses, 30)}}
	}
}

func openInEditorCmd(path string) tea.Cmd {
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg { return nil })
}

func editorCommand(path string) *exec.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		if _, err := exec.LookPath("nvim"); err == nil {
			editor = "nvim"
		} else {
			editor = "vi"
		}
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func journalTodayPath(journalDir string) string {
	return journalDir + "/" + time.Now().Format("2006-01-02") + ".md"
}

func notesInboxPath(notesDir string) string {
	return notesDir + "/inbox.md"
}

func trimLong(s string, maxLines int) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) <= maxLines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[:maxLines], "\n") + "\n..."
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
