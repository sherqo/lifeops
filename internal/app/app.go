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
	"sort"
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

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┘", " ", "└")
	highlightColor    = lipgloss.Color("205")
	inactiveTabStyle  = lipgloss.NewStyle().Border(inactiveTabBorder, true).BorderForeground(highlightColor).Padding(0, 1)
	activeTabStyle    = inactiveTabStyle.Copy().Border(activeTabBorder, true)
	windowStyle       = lipgloss.NewStyle().BorderForeground(highlightColor).Padding(1, 0).Border(lipgloss.NormalBorder()).UnsetBorderTop()
	docStyle          = lipgloss.NewStyle().Padding(1, 2, 1, 2)
)

var defaultTabs = []string{"Home", "Journal", "Calendar", "Todos", "Notes", "GitHub", "Habits"}

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
	ghCursor       int
	ghSection      int // 0=My PRs, 1=Repos, 2=Reviews, 3=Issues, 4=Account
	githubPRs      []ghPR
	githubRepos    []ghRepo
	githubReviews  []ghReview
	githubIssues   []ghIssue
	githubProfile  ghProfile
	githubActiveRepos []ghActiveRepo
	githubTodayCommits int
	githubStreak int
	githubErr      string
	dashboard     []string
	calendar      []string
	calMonth      time.Time
	calendarICS   []string
	weather       []string
	customTabs    map[string][]string
	weatherLine   string
	homeEvents    []string
	dashStatsReady bool
	dashOpen       int
	dashDone       int
	dashHabitsDone int
	dashHabitsTotal int
	habitCursor   int
	journalCursor int
	notesCursor  int
}

func (m model) tabNames() []string {
	resolved := resolveTabs(m.cfg)
	if len(resolved) == 0 {
		return defaultTabs
	}
	list := make([]string, 0, len(resolved))
	for _, t := range resolved {
		list = append(list, t.Name)
	}
	return list
}

type refreshMsg struct{}
type fastRefreshMsg struct{}    // 1 minute - dashboard, todos
type slowRefreshMsg struct{}     // 10 minutes - weather, github
type verySlowRefreshMsg struct{} // 1 hour - asu
type customRefreshMsg struct{ name string }
type loadedMsg struct {
	tab   string
	lines []string
}
type githubLoadedMsg struct {
	prs     []ghPR
	repos   []ghRepo
	reviews []ghReview
	issues  []ghIssue
	profile ghProfile
	activeRepos []ghActiveRepo
	todayCommits int
	streak int
}
type githubErrorMsg struct{ err string }
type journalRefreshMsg struct{}
type dashboardStatsMsg struct {
	openTodos  int
	doneTodos  int
	habitsDone int
	habitsTotal int
}

type weatherLineMsg struct{ line string }
type homeEventsMsg struct{ lines []string }

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Repo   struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type ghRepo struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Language    string `json:"-"`
}

type ghRepoRaw struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	Description     string `json:"description"`
	Visibility      string `json:"visibility"`
	PrimaryLanguage struct{ Name string } `json:"primaryLanguage"`
}

func (r ghRepoRaw) toGhRepo() ghRepo {
	lang := ""
	if r.PrimaryLanguage.Name != "" {
		lang = r.PrimaryLanguage.Name
	}
	return ghRepo{
		Name:        r.Name,
		URL:         r.URL,
		Description: r.Description,
		Visibility:  r.Visibility,
		Language:    lang,
	}
}

type ghReview struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Repo   string `json:"-"`
	Author string `json:"author"`
}

type ghReviewRaw struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Author     string `json:"author"`
	Repository struct{ NameWithOwner string } `json:"repository"`
}

func (r ghReviewRaw) toGhReview() ghReview {
	return ghReview{
		Number: r.Number,
		Title:  r.Title,
		URL:    r.URL,
		Repo:   r.Repository.NameWithOwner,
		Author: r.Author,
	}
}

type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Repo   string `json:"-"`
	State  string `json:"state"`
}

type ghIssueRaw struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Repository struct{ NameWithOwner string } `json:"repository"`
}

type ghProfile struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Followers   int    `json:"followers"`
	Following   int    `json:"following"`
	PublicRepos int    `json:"public_repos"`
	URL         string `json:"html_url"`
}

type ghActiveRepo struct {
	Name string
	URL  string
}

type ghCommitSearchItem struct {
	Repository struct {
		FullName string `json:"fullName"`
		URL      string `json:"url"`
	} `json:"repository"`
}

func (r ghIssueRaw) toGhIssue() ghIssue {
	return ghIssue{
		Number: r.Number,
		Title:  r.Title,
		URL:    r.URL,
		Repo:   r.Repository.NameWithOwner,
		State:  r.State,
	}
}

var defaultCalendarICS []string

type resolvedTab struct {
	Name           string
	Type           string
	Command        []string
	RefreshMinutes int
	Hint           string
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
	calendarICS := cfg.CalendarICS
	if len(calendarICS) == 0 {
		calendarICS = defaultCalendarICS
	}

	resolved := resolveTabs(cfg)

	in := textinput.New()
	in.Placeholder = "Type and press Enter"
	in.Prompt = "> "
	cmd := textinput.New()
	cmd.Placeholder = "q | refresh | tab <name> | set-journal <path>"
	cmd.Prompt = ":"

	m := model{dataDir: dataDir, notesDir: notesDir, journalDir: journalDir, cfg: cfg, db: db, input: in, command: cmd, status: "q quit | : command | ? help", calMonth: firstOfMonth(time.Now()), calendarICS: calendarICS, height: 24, customTabs: map[string][]string{}, ghSection: 4}
	for _, tab := range resolved {
		if tab.Type == "command" {
			m.customTabs[tab.Name] = []string{"Loading..."}
		}
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func resolveTabs(cfg *config.Config) []resolvedTab {
	var out []resolvedTab
	seen := map[string]bool{}

	if len(cfg.Tabs) == 0 {
		for _, name := range defaultTabs {
			out = append(out, resolvedTab{Name: name, Type: "builtin"})
			seen[strings.ToLower(name)] = true
		}
		return out
	}

	for _, t := range cfg.Tabs {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true

		typeVal := strings.TrimSpace(strings.ToLower(t.Type))
		if typeVal == "" {
			typeVal = "builtin"
		}
		if typeVal == "command" && len(t.Command) == 0 {
			continue
		}
		out = append(out, resolvedTab{
			Name:           name,
			Type:           typeVal,
			Command:        t.Command,
			RefreshMinutes: t.RefreshMinutes,
			Hint:           t.Hint,
		})
	}

	return out
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tickFast(), tickSlow(), tickVerySlow())
	cmds = append(cmds,
		loadDashboardWithStats(m.db),
		loadTodos(m.db),
		loadCalendar(m.calMonth, m.calendarICS),
		loadGitHub(),
	)
	cmds = append(cmds, loadWeather())
	cmds = append(cmds, loadHomeEvents(m.calendarICS))
	for _, t := range resolveTabs(m.cfg) {
		if t.Type == "command" {
			cmds = append(cmds, loadCustomTab(t.Name, t.Command))
			if t.RefreshMinutes > 0 {
				cmds = append(cmds, customTick(t.Name, t.RefreshMinutes))
			}
		}
	}
	return tea.Batch(cmds...)
}

func tickFast() tea.Cmd  { return tea.Tick(1*time.Minute, func(time.Time) tea.Msg { return fastRefreshMsg{} }) }
func tickSlow() tea.Cmd  { return tea.Tick(30*time.Minute, func(time.Time) tea.Msg { return slowRefreshMsg{} }) }
func tickVerySlow() tea.Cmd { return tea.Tick(1*time.Hour, func(time.Time) tea.Msg { return verySlowRefreshMsg{} }) }

func customTick(name string, minutes int) tea.Cmd {
	if minutes <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(minutes)*time.Minute, func(time.Time) tea.Msg { return customRefreshMsg{name: name} })
}

func loadTab(tab string, db *store.DB, month time.Time, feeds []string) tea.Cmd {
	switch tab {
	case "Dashboard":
		return loadDashboardWithStats(db)
	case "Calendar":
		return loadCalendar(month, feeds)
	case "GitHub":
		return loadGitHub()
	case "Todos":
		return loadTodos(db)
	default:
		return nil
	}
}

func loadAll(db *store.DB, month time.Time, feeds []string) tea.Cmd {
	return tea.Batch(loadDashboard(), loadCalendar(month, feeds), loadGitHub(), loadTodos(db))
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
				active := m.tabNames()[m.tab]
				switch active {
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
				tabs := m.tabNames()
				return m, loadTab(tabs[m.tab], m.db, m.calMonth, m.calendarICS)
			}
		case "l":
			if m.tab < len(m.tabNames())-1 {
				m.tab++
				tabs := m.tabNames()
				return m, loadTab(tabs[m.tab], m.db, m.calMonth, m.calendarICS)
			}
	case "r":
		m.status = "refreshing..."
		tabs := m.tabNames()
		active := tabs[m.tab]
		if cmd := loadTab(active, m.db, m.calMonth, m.calendarICS); cmd != nil {
			return m, cmd
		}
		for _, t := range resolveTabs(m.cfg) {
			if t.Type == "command" && strings.EqualFold(t.Name, active) {
				return m, loadCustomTab(t.Name, t.Command)
			}
		}
		return m, nil
		case "n":
			if m.tabNames()[m.tab] == "Calendar" {
				m.calMonth = m.calMonth.AddDate(0, 1, 0)
				m.status = "calendar next month"
				return m, loadCalendar(m.calMonth, m.calendarICS)
			}
		case "p":
			if m.tabNames()[m.tab] == "Calendar" {
				m.calMonth = m.calMonth.AddDate(0, -1, 0)
				m.status = "calendar previous month"
				return m, loadCalendar(m.calMonth, m.calendarICS)
			}
		case "T":
			if m.tabNames()[m.tab] == "Calendar" {
				m.calMonth = firstOfMonth(time.Now())
				m.status = "calendar current month"
				return m, loadCalendar(m.calMonth, m.calendarICS)
			}
		case "a":
			active := m.tabNames()[m.tab]
			if active == "Todos" || active == "Notes" || active == "Journal" {
				m.mode = modeInput
				m.input.Focus()
				m.status = "enter text and press Enter"
			}
		case "e":
			active := m.tabNames()[m.tab]
			if active == "Journal" {
				m.status = "opening journal in editor"
				return m, openInEditorCmd(selectedJournalPath(m.journalDir, m.journalCursor))
			}
			if active == "Notes" {
				m.status = "opening notes in editor"
				return m, openInEditorCmd(selectedNotesPath(m.notesDir, m.notesCursor))
			}
		case "w":
			if m.tabNames()[m.tab] == "Journal" {
				_ = os.MkdirAll(m.journalDir, 0o755)
				m.status = "opening current week journal"
				return m, openInEditorCmd(currentWeekJournalPath(m.journalDir))
			}
		case "j":
			m = moveDown(m)
		case "k":
			m = moveUp(m)
		case "x":
			if m.tabNames()[m.tab] == "Todos" && store.ToggleVisibleTodo(m.db, m.todoFilter, m.todoCursor) {
				_ = store.Save(m.dataDir, m.db)
				m.status = "todo toggled"
			}
		case "f":
			if m.tabNames()[m.tab] == "Todos" {
				m.todoFilter = store.NextFilter(m.todoFilter)
				m.todoCursor = 0
				m.status = "todo filter: " + store.FilterLabel(m.todoFilter)
			}
		case "o", "enter":
			active := m.tabNames()[m.tab]
			if active == "Journal" {
				m.status = "opening journal in editor"
				return m, openInEditorCmd(selectedJournalPath(m.journalDir, m.journalCursor))
			}
			if active == "Notes" {
				m.status = "opening notes in editor"
				return m, openInEditorCmd(selectedNotesPath(m.notesDir, m.notesCursor))
			}
			if active == "GitHub" {
				switch m.ghSection {
				case 0:
					if len(m.githubPRs) > 0 {
						pr := m.githubPRs[m.ghCursor]
						if pr.URL != "" {
							m.status = "PR URL: " + pr.URL
						} else {
							m.status = "no PR URL available"
						}
					}
				case 1:
					if len(m.githubRepos) > 0 {
						r := m.githubRepos[m.ghCursor]
						m.status = "Repo URL: " + r.URL
					}
				case 2:
					if len(m.githubReviews) > 0 {
						r := m.githubReviews[m.ghCursor]
						m.status = "Review URL: " + r.URL
					}
				case 3:
					if len(m.githubIssues) > 0 {
						i := m.githubIssues[m.ghCursor]
						m.status = "Issue URL: " + i.URL
					}
				case 4:
					if len(m.githubActiveRepos) > 0 {
						r := m.githubActiveRepos[m.ghCursor]
						m.status = "Repo URL: " + r.URL
					} else if m.githubProfile.URL != "" {
						m.status = "Profile URL: " + m.githubProfile.URL
					}
				}
			}
			for _, t := range resolveTabs(m.cfg) {
				if t.Type == "command" && strings.EqualFold(t.Name, active) {
					m.status = "running command..."
					return m, loadCustomTab(t.Name, t.Command)
				}
			}
		case "t":
			if m.tabNames()[m.tab] == "GitHub" && len(m.githubPRs) > 0 {
				pr := m.githubPRs[m.ghCursor]
				store.AddTodo(m.db, fmt.Sprintf("Review PR #%d: %s", pr.Number, pr.Title))
				_ = store.Save(m.dataDir, m.db)
				m.status = "todo created from PR"
			}
		case " ":
			if m.tabNames()[m.tab] == "Habits" && len(m.db.Habits) > 0 {
				i := m.habitCursor
				m.db.Habits[i].Completed = !m.db.Habits[i].Completed
				_ = store.Save(m.dataDir, m.db)
				m.status = "habit toggled"
			}
		case "[":
			if m.tabNames()[m.tab] == "GitHub" && m.ghSection > 0 {
				m.ghSection--
				m.ghCursor = 0
			}
		case "]":
			if m.tabNames()[m.tab] == "GitHub" && m.ghSection < 4 {
				m.ghSection++
				m.ghCursor = 0
			}
		}
	case fastRefreshMsg:
		return m, tea.Batch(loadDashboardWithStats(m.db), loadTodos(m.db), tickFast())
	case slowRefreshMsg:
		return m, tea.Batch(loadWeather(), loadHomeEvents(m.calendarICS), tickSlow())
	case verySlowRefreshMsg:
		return m, tickVerySlow()
	case customRefreshMsg:
		for _, tab := range resolveTabs(m.cfg) {
			if tab.Type == "command" && strings.EqualFold(tab.Name, t.name) {
				return m, tea.Batch(loadCustomTab(tab.Name, tab.Command), customTick(tab.Name, tab.RefreshMinutes))
			}
		}
		return m, nil
	case loadedMsg:
		if t.tab == "Dashboard" {
			m.dashboard = t.lines
		}
		if t.tab == "Calendar" {
			m.calendar = t.lines
		}
		if _, ok := m.customTabs[t.tab]; ok {
			m.customTabs[t.tab] = t.lines
		}
	case githubLoadedMsg:
		m.githubPRs = t.prs
		m.githubRepos = t.repos
		m.githubReviews = t.reviews
		m.githubIssues = t.issues
		m.githubProfile = t.profile
		m.githubActiveRepos = t.activeRepos
		m.githubTodayCommits = t.todayCommits
		m.githubStreak = t.streak
		m.githubErr = ""
	case githubErrorMsg:
		m.githubErr = t.err
		m.status = "GitHub error: " + t.err
	case journalRefreshMsg:
		m.status = "journal refreshed"
	case dashboardStatsMsg:
		m.dashStatsReady = true
		m.dashOpen = t.openTodos
		m.dashDone = t.doneTodos
		m.dashHabitsDone = t.habitsDone
		m.dashHabitsTotal = t.habitsTotal
		m.dashboard = buildDashboardWithWeather(m, t)
		m.status = "dashboard updated"
	case weatherLineMsg:
		m.weatherLine = t.line
		if m.dashStatsReady {
			m.dashboard = buildDashboardWithWeather(m, dashboardStatsMsg{
				openTodos:  m.dashOpen,
				doneTodos:  m.dashDone,
				habitsDone: m.dashHabitsDone,
				habitsTotal: m.dashHabitsTotal,
			})
		}
	case homeEventsMsg:
		m.homeEvents = t.lines
		if m.dashStatsReady {
			m.dashboard = buildDashboardWithWeather(m, dashboardStatsMsg{
				openTodos:  m.dashOpen,
				doneTodos:  m.dashDone,
				habitsDone: m.dashHabitsDone,
				habitsTotal: m.dashHabitsTotal,
			})
		}
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
		cmds := []tea.Cmd{loadAll(m.db, m.calMonth, m.calendarICS)}
		for _, t := range resolveTabs(m.cfg) {
			if t.Type == "command" {
				cmds = append(cmds, loadCustomTab(t.Name, t.Command))
			}
		}
		return tea.Batch(cmds...)
	case "tab":
		if len(parts) > 1 {
			name := strings.ToLower(strings.Join(parts[1:], " "))
			for i, t := range m.tabNames() {
				if strings.ToLower(t) == name {
					m.tab = i
					if cmd := loadTab(t, m.db, m.calMonth, m.calendarICS); cmd != nil {
						return cmd
					}
					for _, ct := range resolveTabs(m.cfg) {
						if ct.Type == "command" && strings.EqualFold(ct.Name, t) {
							return loadCustomTab(ct.Name, ct.Command)
						}
					}
					return nil
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
	if m.tabNames()[m.tab] == "GitHub" {
		var max int
		switch m.ghSection {
		case 0:
			max = len(m.githubPRs)
		case 1:
			max = len(m.githubRepos)
		case 2:
			max = len(m.githubReviews)
		case 3:
			max = len(m.githubIssues)
		case 4:
			max = len(m.githubActiveRepos)
		}
		if max > 0 && m.ghCursor < max-1 {
			m.ghCursor++
		}
	}
	if m.tabNames()[m.tab] == "Todos" {
		n := len(store.VisibleTodoIndices(m.db, m.todoFilter))
		if m.todoCursor < n-1 {
			m.todoCursor++
		}
	}
	if m.tabNames()[m.tab] == "Habits" && m.habitCursor < len(m.db.Habits)-1 {
		m.habitCursor++
	}
	if m.tabNames()[m.tab] == "Journal" {
		n := len(journalFilesForDisplay(m.journalDir))
		if m.journalCursor < n-1 {
			m.journalCursor++
		}
	}
	if m.tabNames()[m.tab] == "Notes" {
		n := len(notesFilesForDisplay(m.notesDir))
		if m.notesCursor < n-1 {
			m.notesCursor++
		}
	}
	return m
}

func moveUp(m model) model {
	if m.tabNames()[m.tab] == "GitHub" && m.ghCursor > 0 {
		m.ghCursor--
	}
	if m.tabNames()[m.tab] == "Todos" && m.todoCursor > 0 {
		m.todoCursor--
	}
	if m.tabNames()[m.tab] == "Habits" && m.habitCursor > 0 {
		m.habitCursor--
	}
	if m.tabNames()[m.tab] == "Journal" && m.journalCursor > 0 {
		m.journalCursor--
	}
	if m.tabNames()[m.tab] == "Notes" && m.notesCursor > 0 {
		m.notesCursor--
	}
	return m
}

func (m model) View() string {
	// Colored tabs - active with brackets and color
	var tabBar string
	resolvedTabs := m.tabNames()
	for i, t := range resolvedTabs {
		if i == m.tab {
			tabBar += tabActive.Render("["+t+"]") + " "
		} else {
			tabBar += tabInactive.Render(t) + " "
		}
	}
	tabBar = strings.TrimSpace(tabBar)

	// Build body content - plain text
	body := strings.Join(m.currentTab(), "\n")
	if m.height > 0 {
		maxBody := m.height - 7 // tabs, divider, hint, controls, status
		if maxBody < 5 {
			maxBody = 5
		}
		parts := strings.Split(body, "\n")
		if len(parts) > maxBody {
			parts = parts[:maxBody]
			parts[len(parts)-1] = subtext.Render("... (content clipped)")
			body = strings.Join(parts, "\n")
		}
	}
	if m.mode == modeInput {
		body += "\n\n" + m.input.View()
	}
	if m.mode == modeCommand {
		body += "\n\n" + m.command.View()
	}

	// Divider line with color
	dividerStr := divider.Render(strings.Repeat("─", max(20, m.width-2)))

	// Tab-specific hints
	hint := ""
	if h := lookupTabHint(m.cfg, resolvedTabs[m.tab]); h != "" {
		hint = subtext.Render(h) + "\n"
	}

	// General controls at bottom
	controls := subtext.Render("h/l: tabs | j/k: move | r: refresh | ?: help | q: quit")
	status := statusBar.Render(" " + m.status)

	// Padding to push controls to bottom
	padding := ""
	if m.height > 0 {
		lines := strings.Count(body, "\n") + 1
		need := m.height - lines - 5 // +1 for hint
		if need > 0 {
			padding = strings.Repeat("\n", need)
		}
	} else {
		padding = "\n\n"
	}

	view := tabBar + "\n" + dividerStr + "\n" + body + "\n" + padding + hint + controls + "\n" + status
	return view
}

func (m model) currentTab() []string {
	active := m.tabNames()[m.tab]
	switch active {
	case "Home":
		return m.dashboard
	case "Calendar":
		return m.calendar
	case "Todos":
		return renderTodos(m.db, m.todoFilter, m.todoCursor)
	case "Journal":
		return renderJournal(m.journalDir, m.journalCursor)
	case "Notes":
		return renderNotes(m.notesDir, m.notesCursor)
	case "GitHub":
		return renderGitHub(m.githubPRs, m.githubRepos, m.githubReviews, m.githubIssues, m.githubProfile, m.githubActiveRepos, m.githubTodayCommits, m.githubStreak, m.ghSection, m.ghCursor, m.githubErr)
	default:
		if lines, ok := m.customTabs[active]; ok {
			return lines
		}
		return renderHabits(m.db.Habits, m.habitCursor)
	}
}

func lookupTabHint(cfg *config.Config, name string) string {
	if cfg == nil {
		return ""
	}
	for _, t := range cfg.Tabs {
		if strings.EqualFold(t.Name, name) && t.Hint != "" {
			return t.Hint
		}
	}
	switch name {
	case "Calendar":
		return "n/p: month | T: today"
	case "Todos":
		return "x: toggle | f: filter"
	case "Journal":
		return "a: add | e: edit | w: this week"
	case "Notes":
		return "a: add | e: edit"
	case "GitHub":
		return "[:] sections | Enter: open | t: todo"
	case "Habits":
		return "space: toggle"
	default:
		return ""
	}
}

func loadCustomTab(name string, cmd []string) tea.Cmd {
	return func() tea.Msg {
		if len(cmd) == 0 {
			return loadedMsg{tab: name, lines: []string{"No command configured"}}
		}
		out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			return loadedMsg{tab: name, lines: []string{"Command failed:", err.Error(), trimLong(string(out), 40)}}
		}
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
			lines = []string{"No output"}
		}
		return loadedMsg{tab: name, lines: lines}
	}
}

func renderJournal(journalDir string, cursor int) []string {
	files := journalFilesForDisplay(journalDir)
	lines := []string{}

	if _, err := os.Stat(journalDir); os.IsNotExist(err) {
		lines = append(lines, errorText.Render("ERROR: Journal directory does not exist: "+journalDir))
		lines = append(lines, subtext.Render("Use :set-journal <path> to set a valid path"))
	} else {
		lines = append(lines, subtext.Render("Files: "+fmt.Sprintf("%d", len(files))))
	}

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
		lines = append(lines, subtext.Render("Files: "+fmt.Sprintf("%d", len(files))))
	}

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
	lines := []string{subtext.Render("filter: " + store.FilterLabel(filter))}
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
	var lines []string
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

func renderGitHub(prs []ghPR, repos []ghRepo, reviews []ghReview, issues []ghIssue, profile ghProfile, activeRepos []ghActiveRepo, todayCommits int, streak int, section, cursor int, errMsg string) []string {
	var lines []string

	sectionNames := []string{"My PRs", "Repositories", "Reviews", "Issues", "Account"}
	lines = append(lines, header.Render(sectionNames[section]))
	lines = append(lines, subtext.Render("Use [ and ] to switch sections"))
	lines = append(lines, "")

	if errMsg != "" {
		return append(lines, errorText.Render("Error: "+errMsg))
	}

	switch section {
	case 0: // My PRs
		if len(prs) == 0 {
			lines = append(lines, subtext.Render("No open pull requests"))
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
	case 1: // Repos
		if len(repos) == 0 {
			lines = append(lines, subtext.Render("No repositories"))
		}
		for i, r := range repos {
			var itemStyle lipgloss.Style
			if i == cursor {
				itemStyle = selected.Bold(true)
			} else {
				itemStyle = normalItem
			}
			vis := subtext.Render("[" + r.Visibility + "]")
			lang := ""
			if r.Language != "" {
				lang = subtext.Render(" " + r.Language)
			}
			lines = append(lines, itemStyle.Render(r.Name)+" "+vis+lang)
			if r.Description != "" {
				lines[len(lines)-1] += " " + subtext.Render("- "+trimOneLine(r.Description, 60))
			}
		}
	case 2: // Reviews
		if len(reviews) == 0 {
			lines = append(lines, subtext.Render("No reviews requested"))
		}
		for i, r := range reviews {
			var itemStyle lipgloss.Style
			if i == cursor {
				itemStyle = selected.Bold(true)
			} else {
				itemStyle = normalItem
			}
			lines = append(lines, itemStyle.Render(fmt.Sprintf("#%d %s (%s by %s)", r.Number, r.Title, r.Repo, r.Author)))
		}
	case 3: // Issues
		if len(issues) == 0 {
			lines = append(lines, subtext.Render("No assigned issues"))
		}
		for i, iss := range issues {
			var itemStyle lipgloss.Style
			if i == cursor {
				itemStyle = selected.Bold(true)
			} else {
				itemStyle = normalItem
			}
			lines = append(lines, itemStyle.Render(fmt.Sprintf("#%d %s (%s)", iss.Number, iss.Title, iss.Repo)))
		}
	case 4: // Account
		name := profile.Login
		if profile.Name != "" {
			name = profile.Name + " (@" + profile.Login + ")"
		}
		if name != "" {
			lines = append(lines, normalItem.Render("Account: "+name))
		}
		if profile.PublicRepos > 0 {
			lines = append(lines, subtext.Render(fmt.Sprintf("Public repos: %d", profile.PublicRepos)))
		}
		lines = append(lines, normalItem.Render(fmt.Sprintf("Today commits: %d | Current streak: %d days", todayCommits, streak)))
		lines = append(lines, "")
		lines = append(lines, header.Render("Recently active repos"))
		if len(activeRepos) == 0 {
			lines = append(lines, subtext.Render("No recent commit activity"))
		}
		for i, r := range activeRepos {
			var itemStyle lipgloss.Style
			if i == cursor {
				itemStyle = selected.Bold(true)
			} else {
				itemStyle = normalItem
			}
			lines = append(lines, itemStyle.Render(r.Name))
		}
	}

	return lines
}

func loadDashboardWithStats(db *store.DB) tea.Cmd {
	return loadDashboardStats(db)
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

func buildDashboardWithWeather(m model, stats dashboardStatsMsg) []string {
	var lines []string
	now := time.Now()
	host, _ := os.Hostname()
	
	lines = append(lines, header.Render(now.Format("15:04"))+" "+subtext.Render(now.Format("Monday, January 2, 2006")))
	if m.weatherLine != "" {
		lines = append(lines, "")
		lines = append(lines, m.weatherLine)
	}
	if len(m.homeEvents) > 0 {
		lines = append(lines, "")
		lines = append(lines, subtext.Render("Next 3 days:"))
		for _, ev := range m.homeEvents {
			lines = append(lines, ev)
		}
	}
	lines = append(lines, divider.Render(""))
	
	lines = append(lines, header.Render("Machine"))
	osInfo := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	machineParts := []string{"Host: " + host, "OS: " + osInfo}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		load := strings.TrimSpace(string(b))
		parts := strings.Fields(load)
		if len(parts) > 0 {
			machineParts = append(machineParts, "Load: "+parts[0])
		}
	}
	if mem, err := parseMem(); err == nil {
		machineParts = append(machineParts, fmt.Sprintf("Memory: %.1f/%.1f GiB", mem[0], mem[1]))
	}
	lines = append(lines, normalItem.Render(strings.Join(machineParts, " • ")))
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
			cell = lipgloss.NewStyle().Foreground(lipgloss.Color("76")).Bold(true).Render(cell)
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

func loadHomeEvents(feeds []string) tea.Cmd {
	return func() tea.Msg {
		if len(feeds) == 0 {
			return homeEventsMsg{lines: nil}
		}
		start := time.Now()
		end := start.AddDate(0, 0, 4)
		events := collectUpcomingEvents(feeds, start, end, 6)
		if len(events) == 0 {
			return homeEventsMsg{lines: []string{subtext.Render("- No upcoming events")}}
		}
		var lines []string
		for _, e := range events {
			lines = append(lines, normalItem.Render("- "+e.start.Format("Mon 15:04")+" ")+subtext.Render(e.summary))
		}
		return homeEventsMsg{lines: lines}
	}
}

type calEvent struct {
	start   time.Time
	summary string
}

func collectUpcomingEvents(feeds []string, start, end time.Time, limit int) []calEvent {
	var out []calEvent
	for _, feed := range feeds {
		res, err := http.Get(feed)
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			continue
		}
		out = append(out, parseICSRange(string(raw), start, end)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start.Before(out[j].start) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func parseICSRange(ics string, start, end time.Time) []calEvent {
	lines := strings.Split(ics, "\n")
	var out []calEvent
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
		if !t.Before(start) && t.Before(end) {
			out = append(out, calEvent{start: t, summary: summary})
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
			return weatherLineMsg{line: subtext.Render("Weather unavailable")}
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
			return weatherLineMsg{line: subtext.Render("Weather unavailable")}
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
		weatherLine := header.Render(place) + " " + normalItem.Render("• "+c.TempC+"C feels "+c.FeelsLikeC+"C") + " " + subtext.Render("• "+d+" • "+c.Humidity+"%")
		return weatherLineMsg{line: weatherLine}
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

		// Load My PRs
		raw := run("gh", "search", "prs", "--author", "@me", "--state", "open", "--limit", "15", "--json", "number,title,url,repository")
		if strings.TrimSpace(raw) == "" {
			raw = "[]"
		}
		var prs []ghPR
		if err := json.Unmarshal([]byte(raw), &prs); err != nil {
			return githubErrorMsg{err: "failed to parse PRs: " + raw}
		}

		// Load Repos
		raw = run("gh", "repo", "list", "--limit", "20", "--json", "name,url,description,visibility,primaryLanguage")
		var rawRepos []ghRepoRaw
		if err := json.Unmarshal([]byte(raw), &rawRepos); err != nil {
			return githubErrorMsg{err: "failed to parse repos"}
		}
		var repos []ghRepo
		for _, r := range rawRepos {
			repos = append(repos, r.toGhRepo())
		}

		// Load Reviews requested
		raw = run("gh", "search", "prs", "--review-requested", "@me", "--state", "open", "--limit", "15", "--json", "number,title,url,repository,author")
		var rawReviews []ghReviewRaw
		if err := json.Unmarshal([]byte(raw), &rawReviews); err != nil {
			return githubErrorMsg{err: "failed to parse reviews"}
		}
		var reviews []ghReview
		for _, r := range rawReviews {
			reviews = append(reviews, r.toGhReview())
		}

		// Load Issues assigned
		raw = run("gh", "search", "issues", "--assignee", "@me", "--state", "open", "--limit", "15", "--json", "number,title,url,repository,state")
		var rawIssues []ghIssueRaw
		if err := json.Unmarshal([]byte(raw), &rawIssues); err != nil {
			return githubErrorMsg{err: "failed to parse issues"}
		}
		var issues []ghIssue
		for _, r := range rawIssues {
			issues = append(issues, r.toGhIssue())
		}

		// Account profile
		raw = run("gh", "api", "user")
		var profile ghProfile
		_ = json.Unmarshal([]byte(raw), &profile)

		// Today commits and recently active repos
		today := time.Now().Format("2006-01-02")
		raw = run("gh", "search", "commits", "--author", "@me", "--author-date", ">="+today, "--limit", "100", "--json", "sha,repository")
		var todayCommits []ghCommitSearchItem
		_ = json.Unmarshal([]byte(raw), &todayCommits)

		since := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
		raw = run("gh", "search", "commits", "--author", "@me", "--author-date", ">="+since, "--limit", "200", "--json", "sha,repository")
		var recentCommits []ghCommitSearchItem
		_ = json.Unmarshal([]byte(raw), &recentCommits)
		seenRepos := map[string]bool{}
		activeRepos := make([]ghActiveRepo, 0, 8)
		for _, c := range recentCommits {
			name := c.Repository.FullName
			if name == "" || seenRepos[name] {
				continue
			}
			seenRepos[name] = true
			activeRepos = append(activeRepos, ghActiveRepo{Name: name, URL: c.Repository.URL})
			if len(activeRepos) >= 8 {
				break
			}
		}

		streak := githubCurrentStreak(profile.Login)

		return githubLoadedMsg{
			prs:          prs,
			repos:        repos,
			reviews:      reviews,
			issues:       issues,
			profile:      profile,
			activeRepos:  activeRepos,
			todayCommits: len(todayCommits),
			streak:       streak,
		}
	}
}

func githubCurrentStreak(login string) int {
	if strings.TrimSpace(login) == "" {
		return 0
	}
	query := "query($login:String!){ user(login:$login){ contributionsCollection { contributionCalendar { weeks { contributionDays { date contributionCount } } } } } }"
	raw := run("gh", "api", "graphql", "-f", "query="+query, "-f", "login="+login)
	var payload struct {
		Data struct {
			User struct {
				ContributionsCollection struct {
					ContributionCalendar struct {
						Weeks []struct {
							ContributionDays []struct {
								Date  string `json:"date"`
								Count int    `json:"contributionCount"`
							} `json:"contributionDays"`
						} `json:"weeks"`
					} `json:"contributionCalendar"`
				} `json:"contributionsCollection"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0
	}
	dayCounts := map[string]int{}
	for _, w := range payload.Data.User.ContributionsCollection.ContributionCalendar.Weeks {
		for _, d := range w.ContributionDays {
			dayCounts[d.Date] = d.Count
		}
	}
	streak := 0
	for i := 0; i < 365; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		if dayCounts[day] > 0 {
			streak++
			continue
		}
		break
	}
	return streak
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

func currentWeekJournalPath(journalDir string) string {
	isoYear, isoWeek := time.Now().ISOWeek()
	prefix := fmt.Sprintf("week-%02d", isoWeek)
	matches, _ := filepath.Glob(filepath.Join(journalDir, prefix+"*"))
	if len(matches) > 0 {
		sort.Strings(matches)
		return matches[0]
	}
	return filepath.Join(journalDir, fmt.Sprintf("%s-%d.md", prefix, isoYear))
}

func trimLong(s string, maxLines int) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) <= maxLines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[:maxLines], "\n") + "\n..."
}

func trimOneLine(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
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
