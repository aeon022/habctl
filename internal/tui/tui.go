package tui

import (
	"context"
	"os"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/config"
	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
	"github.com/aeon022/missionctl-core/uistate"
)

// ── provider table ────────────────────────────────────────────────────────────

type providerEntry struct {
	id      ai.Provider
	label   string
	keyPage string
	envKey  string
}

var providers = []providerEntry{
	{ai.ProviderAnthropic, "Anthropic / Claude", "https://console.anthropic.com/account/keys", "ANTHROPIC_API_KEY"},
	{ai.ProviderOpenAI, "OpenAI / ChatGPT", "https://platform.openai.com/api-keys", "OPENAI_API_KEY"},
	{ai.ProviderGemini, "Google Gemini", "https://aistudio.google.com/apikey", "GEMINI_API_KEY"},
	{ai.ProviderOllama, "Ollama (local)", "", ""},
}

// ── view state ───────────────────────────────────────────────────────────────

type viewState int

const (
	viewList     viewState = iota
	viewAddInput           // step 1: habit name (+ optional emoji prefix)
	viewAddDesc            // step 2: description (optional)
	viewHelp
	viewSuggest
	viewSettings
	viewKeyInput
	viewStats
	viewEditHabit   // edit name/icon/description of selected habit
	viewGroupMgr    // manage groups list
	viewGroupNew    // create new group (name + icon)
	viewGroupPick   // assign a habit to a group
	viewHabitDetail // full habit detail / expand view
	viewReview      // weekly AI coaching briefing
	viewNoteInput   // add/edit note for today's check-in
	viewChainMgr    // manage habit chains
	viewChainPick   // pick target habit when creating a chain
	viewGeminiMenu
	viewGeminiCID
	viewGeminiCS
	viewOAuthWait
	viewArchive     // archived habits list
	viewGoalInput   // goal → 3 decomposed habits
	viewConfirm     // confirmation prompt before a destructive action
	viewFilterInput // "/" filter over the habit list
	viewCommand     // ":" command palette
	viewPresets     // "p" curated habit template picker
)

// doubleClickWindow opens the habit detail view on a second click within
// this window, same pattern and duration taskctl uses for its own
// double-click.
const doubleClickWindow = 400 * time.Millisecond

// undoWindow is how long after a group/chain delete "u" still restores
// it — same duration taskctl uses for its own delete-undo.
const undoWindow = 5 * time.Second

// ── messages ─────────────────────────────────────────────────────────────────

type suggestChunkResult struct {
	text string
	done bool
	err  error
	gen  int
}

type suggestChunkMsg struct {
	text string
	gen  int
}
type suggestDoneMsg struct{ gen int }
type suggestErrMsg struct {
	err error
	gen int
}

type suggestItem struct {
	name     string   // text from **...**  — used when adding to DB
	header   string   // first line without ** (name + time)
	details  []string // description and tip lines
	selected bool
}

type chainSuggestItem struct {
	from     string
	to       string
	reason   string
	selected bool
}

type reviewChunkResult struct {
	text string
	done bool
	err  error
	gen  int
}
type reviewChunkMsg struct {
	text string
	gen  int
}
type reviewDoneMsg struct{ gen int }
type reviewErrMsg struct {
	err error
	gen int
}

type oauthSuccessMsg struct{ refreshToken string }
type oauthErrMsg struct{ err error }

type habitsLoadedMsg []models.HabitStats
type groupsLoadedMsg []models.Group
type chainsLoadedMsg []models.Chain
type errMsg struct{ err error }
type clearMsgMsg struct{}
type clearUndoMsg struct{} // fires undoWindow after a group/chain delete — clears the toast AND the undo capability together, same coupling taskctl uses
type groupDeletedMsg struct {
	group      models.Group
	habitNames []string // habits that were in the group, captured before delete for undo re-linking
}
type chainDeletedMsg struct{ chain models.Chain }
type statusMsg string
type blinkMsg struct{}
type notesLoadedMsg struct {
	name  string
	notes []models.NoteEntry
}
type habitCheckinsLoadedMsg struct {
	name  string
	dates map[string]bool
}
type archivedLoadedMsg []models.Habit
type archiveReloadMsg struct{ status string }

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	habits       []models.HabitStats
	groups       []models.Group
	chains       []models.Chain
	cursor       int
	hoverRow     int // m.habits index under the mouse cursor, -1 when none
	lastClickRow int // m.habits index of the previous left-click, -1 when none — double-click opens the habit detail view, same window/pattern taskctl uses
	lastClickAt  time.Time
	// batch select mode ("V") — bulk-archive, same pattern taskctl's own
	// select mode uses. Named batchSelected/batchMode (not "selected") to
	// avoid colliding with the existing per-row "is this the cursor row"
	// meaning of `selected` used throughout the render code.
	batchMode     bool
	batchSelected map[string]bool // keyed by habit name
	state         viewState
	input         textinput.Model
	s             *store.Store
	message       string
	isErr         bool
	weekView      bool
	height        int

	suggestText    string
	suggestDone    bool
	suggestBlocked bool // true when suggestText is a Bundle-upsell message, not an AI response to parse
	suggestCh      <-chan suggestChunkResult
	suggestGen     int
	suggestCancel  context.CancelFunc
	suggestItems   []suggestItem
	suggestCursor  int

	// weekly review (AI briefing)
	reviewText    string
	reviewDone    bool
	reviewBlocked bool // true when reviewText is a Bundle-upsell message, not an AI response
	reviewCh      <-chan reviewChunkResult
	reviewGen     int
	reviewCancel  context.CancelFunc

	settingsCursor   int
	geminiMenuCursor int
	geminiClientID   string

	// add-habit flow
	addingName string
	addingIcon string

	// edit-habit flow
	editCursor  int
	editOldName string

	// group management
	groupCursor int

	// "/" filter over the habit list
	allHabits []models.HabitStats
	filterQ   string

	// focus reload: lastLoad is when habitsLoadedMsg last arrived; reloading
	// is true while a focus-triggered reload is in flight.
	lastLoad  time.Time
	reloading bool

	// ":" command palette
	cmdCursor int // index into the filtered command matches

	// "?" transient help popup
	helpVP   viewport.Model
	helpPopW int
	helpPopH int

	// confirm-before-delete
	confirmPrompt string
	confirmAction tea.Cmd
	confirmReturn viewState

	// undo: "u" within undoWindow of a group/chain delete restores it —
	// same pattern and window taskctl uses for its own delete-undo.
	lastDeletedGroup       *models.Group
	lastDeletedGroupHabits []string // habits that were in the group, re-linked on undo
	lastDeletedChain       *models.Chain

	// chain management
	chainCursor     int    // cursor in viewChainMgr
	chainPickCursor int    // cursor in viewChainPick
	chainFromName   string // habit name selected as chain source

	// note flow: add note to today's check-in
	noteForHabit string // habit name the note belongs to

	// extended edit-habit fields (buffered across tab switches)
	editNameBuf string
	editDescBuf string
	editFreq    int
	editSkip    int

	// suggest mode: "habit" (default), "chain", or "decompose"
	suggestMode       string
	chainSuggestItems []chainSuggestItem

	// UI state
	compact bool // hide descriptions/notes in list view
	blinkOn bool // streaming cursor blink state

	// detail-view: loaded per-habit recent notes
	recentNotes    []models.NoteEntry
	recentNotesFor string

	// detail-view: loaded per-habit check-in history, for the multi-week
	// heatmap (falls back to HabitStats.Last7Days until this arrives)
	detailHeatmap    map[string]bool // date string ("2006-01-02") -> checked in
	detailHeatmapFor string

	// archive view
	archivedHabits []models.Habit
	archiveCursor  int

	// preset picker ("p")
	presetCursor int

	cfg          config.Config
	calData      store.CalendarData
	checkinDates map[int64]map[string]bool // habit id -> set of dates checked, for correlation insights
	width        int
}

// ── entry point ──────────────────────────────────────────────────────────────

func Run(s *store.Store) error {
	ti := textinput.New()
	ti.Placeholder = "Habit name…"
	ti.CharLimit = 80
	ti.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	cfg, _ := config.Load()

	var state persistedState
	if p, err := store.UIStatePath(); err == nil {
		uistate.Load(p, &state)
	}

	m := model{s: s, input: ti, cfg: cfg, hoverRow: -1, lastClickRow: -1, weekView: state.WeekView, compact: state.Compact}
	// WithFPS(30) + motionThrottleFilter: all-motion mouse mode re-renders on
	// every pixel of movement, which at 60fps can overwhelm the terminal.
	p := tea.NewProgram(m, tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	_, err := p.Run()
	return err
}

// motionThrottleFilter drops MouseMotionMsg messages arriving <16ms apart.
func motionThrottleFilter() func(tea.Model, tea.Msg) tea.Msg {
	var lastMotion time.Time
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.MouseMotionMsg); !ok {
			return msg
		}
		now := time.Now()
		if now.Sub(lastMotion) < 16*time.Millisecond {
			return nil
		}
		lastMotion = now
		return msg
	}
}

// persistedState is what Run restores from and saveUIState saves to — see
// missionctl-core/uistate.
type persistedState struct {
	WeekView bool `json:"week_view"`
	Compact  bool `json:"compact"`
}

func (m model) saveUIState() {
	path, err := store.UIStatePath()
	if err != nil {
		return
	}
	_ = uistate.Save(path, persistedState{WeekView: m.weekView, Compact: m.compact})
}
