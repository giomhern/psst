package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func editingModel(text string) model {
	m := composerModel()
	m.composing = true
	return m.appendDraft(text)
}

func TestComposerEditsAtCursor(t *testing.T) {
	m := editingModel("Hello world")
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "beautiful "})
	if m.draft != "Hello beautiful world" || m.draftCursor != len("Hello beautiful ") {
		t.Fatal("paste was not inserted at cursor")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.draft != "Hello world" || m.draftCursor != len("Hello ") {
		t.Fatal("word deletion did not preserve suffix")
	}
	m, _ = update(m, key(tea.KeyDelete, ""))
	m, _ = update(m, key('W', "W"))
	if m.draft != "Hello World" {
		t.Fatal("forward deletion or insertion failed")
	}
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key(tea.KeyBackspace, ""))
	if m.draftCursor != 0 || m.draft != "Hello World" {
		t.Fatal("backspace at start changed draft")
	}
	m, _ = update(m, key(tea.KeyEnd, ""))
	m, _ = update(m, key(tea.KeyDelete, ""))
	if m.draftCursor != len(m.draft) || m.draft != "Hello World" {
		t.Fatal("delete at end changed draft")
	}
}

func TestComposerMovesAndDeletesWholeGraphemes(t *testing.T) {
	for _, text := range []string{"e\u0301", "👍🏽", "👩‍💻", "🇺🇸", "👨‍👩‍👧‍👦"} {
		t.Run(text, func(t *testing.T) {
			m := editingModel("a" + text + "z")
			m, _ = update(m, key(tea.KeyLeft, ""))
			m, _ = update(m, key(tea.KeyLeft, ""))
			if m.draftCursor != 1 {
				t.Fatal("left split a visible character")
			}
			m, _ = update(m, key(tea.KeyRight, ""))
			m, _ = update(m, key(tea.KeyBackspace, ""))
			if m.draft != "az" || m.draftCursor != 1 {
				t.Fatal("backspace split a visible character")
			}
			m = editingModel(text + "z")
			m, _ = update(m, key(tea.KeyHome, ""))
			m, _ = update(m, key(tea.KeyDelete, ""))
			if m.draft != "z" || m.draftCursor != 0 {
				t.Fatal("delete split a visible character")
			}
		})
	}
}

func TestComposerMultilineAndReview(t *testing.T) {
	m := editingModel("abc\n世界z\nend")
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key(tea.KeyRight, ""))
	m, _ = update(m, key(tea.KeyUp, ""))
	if m.draftCursor != len("abc\n") {
		t.Fatal("vertical navigation landed inside wide character")
	}
	m, _ = update(m, key(tea.KeyEnd, ""))
	m, _ = update(m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "new"})
	want := "abc\n世界z\nnew\nend"
	if m.draft != want {
		t.Fatal("newline insertion changed surrounding text")
	}
	position := m.draftCursor
	m, command := update(m, key(tea.KeyEnter, ""))
	if !m.confirming || command != nil || strings.Contains(strings.Join(m.composerLines(), "\n"), `\n`) {
		t.Fatal("review did not preserve visible logical lines")
	}
	m, _ = update(m, key(tea.KeyDelete, ""))
	m, _ = update(m, tea.PasteMsg{Content: "ignored"})
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.draft != want || m.draftCursor != position || m.confirming {
		t.Fatal("review changed draft or lost editing position")
	}
}

func TestComposerRejectedPasteIsAtomicAtCursor(t *testing.T) {
	m := editingModel(strings.Repeat("a", 4000))
	m, _ = update(m, key(tea.KeyHome, ""))
	for _, text := range []string{"z", "\x00", string([]byte{0xff})} {
		next, _ := update(m, tea.PasteMsg{Content: text})
		if next.draft != m.draft || next.draftCursor != 0 || next.draftError == "" {
			t.Fatal("rejected paste changed draft or cursor")
		}
	}
	m, _ = update(m, tea.PasteMsg{Content: ""})
	if m.draftCursor != 0 {
		t.Fatal("empty paste moved cursor")
	}
}

func TestComposerKeepsCursorVisibleAcrossResize(t *testing.T) {
	m := editingModel(strings.Repeat("long line 👩‍💻 ▏ ", 30) + "\nlast")
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 8}, {Width: 100, Height: 20}} {
		m, _ = update(m, size)
		for _, code := range []rune{tea.KeyHome, tea.KeyUp, tea.KeyEnd, tea.KeyDown} {
			m, _ = update(m, key(code, ""))
			rows, cursor := m.draftLines()
			cursor += len(m.composerLines()) - len(rows)
			if cursor < m.composeOffset || cursor >= m.composeOffset+m.visibleRows() {
				t.Fatal("cursor outside viewport")
			}
			for _, line := range strings.Split(m.View().Content, "\n") {
				if ansi.StringWidth(line) > size.Width {
					t.Fatal("editing exceeded terminal width")
				}
			}
		}
	}
}

func FuzzComposerCursorBoundaries(f *testing.F) {
	f.Add("e\u0301👩‍💻🇺🇸\r\nnext", []byte{0, 0, 3, 4, 5, 6, 7, 8, 9})
	f.Add("🇺x🇸", []byte{0, 0, 2, 1, 10})
	f.Fuzz(func(t *testing.T, text string, actions []byte) {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || utf8.RuneCountInString(text) > 200 || len(actions) > 50 {
			t.Skip()
		}
		m := editingModel(text)
		keys := []tea.KeyPressMsg{key(tea.KeyLeft, ""), key(tea.KeyRight, ""), key(tea.KeyDelete, ""), key(tea.KeyBackspace, ""), key(tea.KeyHome, ""), key(tea.KeyEnd, ""), key(tea.KeyUp, ""), key(tea.KeyDown, ""), {Code: 'w', Mod: tea.ModCtrl}, {Code: tea.KeyRight, Mod: tea.ModCtrl}, key('x', "x")}
		for _, action := range actions {
			m, _ = update(m, keys[int(action)%len(keys)])
			valid := m.draftCursor == 0
			clusters := uniseg.NewGraphemes(m.draft)
			for clusters.Next() {
				_, end := clusters.Positions()
				valid = valid || end == m.draftCursor
			}
			if !valid || !utf8.ValidString(m.draft) {
				t.Fatal("editing left cursor inside grapheme or invalid UTF-8")
			}
		}
	})
}

func TestComposerConfirmsExactlyTheEditedMultilineDraft(t *testing.T) {
	m := editingModel("Hello world")
	calls := 0
	m.send = func(id, text string, request uint64) tea.Cmd {
		calls++
		if id != "7" || text != "Hello\nworld!" {
			t.Fatal("send did not receive exact edited recipient and text")
		}
		return func() tea.Msg { return sendCompleted{request: request} }
	}
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt})
	m, _ = update(m, key(tea.KeyDelete, ""))
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	m, _ = update(m, key('!', "!"))
	m, _ = update(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if calls != 0 {
		t.Fatal("editing sent without review")
	}
	m, _ = update(m, key(tea.KeyEnter, ""))
	m, _ = update(m, key('x', "x"))
	_, command := update(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if calls != 1 || command == nil {
		t.Fatal("confirmation did not create exactly one send command")
	}
}

func TestComposerResegmentsAfterJoiningText(t *testing.T) {
	m := editingModel("🇺x🇸")
	m, _ = update(m, key(tea.KeyLeft, ""))
	m, _ = update(m, key(tea.KeyBackspace, ""))
	if m.draft != "🇺🇸" || m.draftCursor != len(m.draft) {
		t.Fatal("deletion left cursor inside newly joined flag")
	}
	m = editingModel("🇸")
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, tea.PasteMsg{Content: "🇺"})
	if m.draft != "🇺🇸" || m.draftCursor != len(m.draft) {
		t.Fatal("insertion left cursor inside newly joined flag")
	}
}

func TestComposerCRLFLineNavigation(t *testing.T) {
	m := editingModel("ab\r\ncd")
	m, _ = update(m, key(tea.KeyUp, ""))
	if m.draftCursor != 2 {
		t.Fatal("Up split CRLF or missed column")
	}
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key(tea.KeyEnd, ""))
	if m.draftCursor != 2 {
		t.Fatal("End split CRLF")
	}
	m, _ = update(m, key(tea.KeyDown, ""))
	if m.draftCursor != len(m.draft) || m.draft != "ab\r\ncd" {
		t.Fatal("Down changed pasted line endings")
	}
}

func TestComposerRendersEmojiJoinersButEscapesControls(t *testing.T) {
	m := editingModel("👩‍💻\n" + `literal \u200d` + "\x1b[2J\u202e")
	rows, _ := m.draftLines()
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "👩‍💻") || !strings.Contains(text, `literal \\u200d`) || strings.ContainsAny(text, "\x1b\u202e") {
		t.Fatal("emoji joiner was escaped or terminal controls were unescaped")
	}
}

func TestComposerVerticalColumnUsesRenderedEmojiWidth(t *testing.T) {
	m := editingModel("👩‍💻x\nabc")
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key(tea.KeyRight, ""))
	m, _ = update(m, key(tea.KeyRight, ""))
	m, _ = update(m, key(tea.KeyUp, ""))
	if m.draftCursor != len("👩‍💻") {
		t.Fatal("vertical column used escaped joiner width")
	}
}
