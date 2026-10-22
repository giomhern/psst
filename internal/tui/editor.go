package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// draftBoundary returns the preceding boundary, or the first boundary at or
// after position when forward is true. Scanning the whole bounded draft keeps
// context-dependent clusters (such as regional-indicator pairs) intact.
func draftBoundary(text string, position int, forward bool) int {
	if position <= 0 {
		return 0
	}
	previous := 0
	clusters := uniseg.NewGraphemes(text)
	for clusters.Next() {
		_, end := clusters.Positions()
		if forward && end >= position {
			return end
		}
		if !forward && end >= position {
			return previous
		}
		previous = end
	}
	if forward {
		return len(text)
	}
	return previous
}

// Word shortcuts use whitespace-delimited runs, including Unicode whitespace.
func draftWord(text string, cursor int, forward bool) int {
	type cluster struct {
		start, end int
		space      bool
	}
	var parts []cluster
	index := 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		start, end := graphemes.Positions()
		r, _ := utf8.DecodeRuneInString(graphemes.Str())
		parts = append(parts, cluster{start, end, unicode.IsSpace(r)})
		if end <= cursor {
			index++
		}
	}
	step := 1
	if !forward {
		index--
		step = -1
	}
	seenText := false
	for index >= 0 && index < len(parts) {
		part := parts[index]
		if part.space && seenText {
			break
		}
		seenText = seenText || !part.space
		cursor = part.start
		if forward {
			cursor = part.end
		}
		index += step
	}
	return cursor
}

func draftLineEnd(text string, cursor int) int {
	if end := strings.IndexByte(text[cursor:], '\n'); end >= 0 {
		position := cursor + end
		if position > 0 && text[position-1] == '\r' {
			position--
		}
		return position
	}
	return len(text)
}

// Up/Down follow logical lines, retaining the displayed column where possible.
func draftVertical(text string, cursor int, down bool) int {
	start := strings.LastIndex(text[:cursor], "\n") + 1
	column := ansi.StringWidth(draftText(text[start:cursor]))
	var target, end int
	if down {
		newline := strings.IndexByte(text[cursor:], '\n')
		if newline < 0 {
			return cursor
		}
		target = cursor + newline + 1
		end = draftLineEnd(text, target)
	} else {
		if start == 0 {
			return cursor
		}
		end = start - 1
		target = strings.LastIndex(text[:end], "\n") + 1
		if end > target && text[end-1] == '\r' {
			end--
		}
	}
	cursor = target
	width := 0
	clusters := uniseg.NewGraphemes(text[target:end])
	for clusters.Next() {
		width += ansi.StringWidth(draftText(clusters.Str()))
		if width > column {
			break
		}
		_, offset := clusters.Positions()
		cursor = target + offset
	}
	return cursor
}

func (m model) deleteDraft(start, end int) model {
	m.draft = m.draft[:start] + m.draft[end:]
	m.draftCursor = start
	if start > 0 {
		m.draftCursor = draftBoundary(m.draft, start, true)
	}
	m.draftError = ""
	return m
}
