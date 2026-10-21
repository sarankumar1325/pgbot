package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
)

// renderChatMarkdown implements the deliberately small Markdown surface the
// conversational UI needs. Model responses should read like terminal-native
// prose; syntax markers are never useful UI.
func renderChatMarkdown(value string, width int) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) == 0 {
		return ""
	}

	var blocks []string
	var code []string
	inCode := false
	blank := false
	flushCode := func() {
		if len(code) == 0 {
			return
		}
		blocks = append(blocks, chatCodeBlockStyle.Width(max(1, width)).Render(strings.Join(code, "\n")))
		code = nil
	}

	for _, raw := range lines {
		line := strings.TrimRightFunc(raw, unicode.IsSpace)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				flushCode()
			}
			inCode = !inCode
			blank = false
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if trimmed == "" {
			blank = len(blocks) > 0
			continue
		}
		if blank {
			blocks = append(blocks, "")
			blank = false
		}

		switch {
		case strings.HasPrefix(trimmed, "### "):
			blocks = append(blocks, titleStyle.Render(renderInlineMarkdown(strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")))))
		case strings.HasPrefix(trimmed, "## "):
			blocks = append(blocks, titleStyle.Render(renderInlineMarkdown(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))))
		case strings.HasPrefix(trimmed, "# "):
			blocks = append(blocks, titleStyle.Render(renderInlineMarkdown(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")))))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			blocks = append(blocks, mutedStyle.Render("• ")+renderInlineMarkdown(strings.TrimSpace(trimmed[2:])))
		default:
			blocks = append(blocks, renderInlineMarkdown(trimmed))
		}
	}
	if inCode {
		flushCode()
	}

	for len(blocks) > 0 && blocks[len(blocks)-1] == "" {
		blocks = blocks[:len(blocks)-1]
	}
	return lipgloss.NewStyle().Foreground(colorInk).Width(max(1, width)).Render(strings.Join(blocks, "\n"))
}

func renderInlineMarkdown(value string) string {
	var result strings.Builder
	var segment strings.Builder
	bold := false
	code := false
	flush := func() {
		if segment.Len() == 0 {
			return
		}
		text := segment.String()
		segment.Reset()
		switch {
		case code:
			result.WriteString(chatInlineCodeStyle.Render(text))
		case bold:
			result.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorInk).Render(text))
		default:
			result.WriteString(text)
		}
	}

	for index := 0; index < len(value); {
		if !code && index+1 < len(value) && value[index:index+2] == "**" {
			flush()
			bold = !bold
			index += 2
			continue
		}
		if value[index] == '`' {
			flush()
			code = !code
			index++
			continue
		}
		segment.WriteByte(value[index])
		index++
	}
	flush()
	return result.String()
}

func renderUserTurn(value string, width int) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for index := range lines {
		prefix := "  "
		if index == 0 {
			prefix = chatPromptStyle.Render("› ")
		}
		lines[index] = prefix + strings.TrimSpace(lines[index])
	}
	return lipgloss.NewStyle().Foreground(colorInk).Width(max(1, width)).Render(strings.Join(lines, "\n"))
}
