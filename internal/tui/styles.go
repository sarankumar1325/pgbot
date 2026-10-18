package tui

import "charm.land/lipgloss/v2"

var (
	colorCanvas   = lipgloss.Color("#0A0A0A")
	colorInk      = lipgloss.Color("#EEEEEE")
	colorMuted    = lipgloss.Color("#808080")
	colorBlue     = lipgloss.Color("#5C9CF5")
	colorCyan     = lipgloss.Color("#FAB283")
	colorGreen    = lipgloss.Color("#7FD88F")
	colorAmber    = lipgloss.Color("#F5A742")
	colorRed      = lipgloss.Color("#E06C75")
	colorPanel    = lipgloss.Color("#141414")
	colorPanelAlt = lipgloss.Color("#1E1E1E")
	colorBorder   = lipgloss.Color("#484848")
	colorRule     = lipgloss.Color("#282828")

	brandStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	badgeStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorCanvas).Background(colorCyan).Padding(0, 1)
	dangerBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(colorCanvas).Background(colorAmber).Padding(0, 1)
	subtitleStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	heroStyle        = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	overlineStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	labelStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	mutedStyle       = lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle       = lipgloss.NewStyle().Foreground(colorRed)
	warnStyle        = lipgloss.NewStyle().Foreground(colorAmber)
	goodStyle        = lipgloss.NewStyle().Foreground(colorGreen)
	keyStyle         = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	panelStyle       = lipgloss.NewStyle().Background(colorPanel).
				Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
	selectedPanelStyle  = panelStyle.BorderForeground(colorBlue).Background(colorPanelAlt)
	dangerPanelStyle    = panelStyle.BorderForeground(colorAmber)
	codeStyle           = lipgloss.NewStyle().Foreground(colorInk).Background(colorCanvas).Padding(1, 2)
	inactiveActionStyle = lipgloss.NewStyle().Foreground(colorMuted)
	activeActionStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	statusNameStyle     = lipgloss.NewStyle().Foreground(colorMuted)
	statusValueStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	tabStyle            = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	activeTabStyle      = lipgloss.NewStyle().Bold(true).Foreground(colorCanvas).Background(colorCyan).Padding(0, 1)
	assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	userLabelStyle      = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	commandStyle        = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	chatComposerStyle   = lipgloss.NewStyle().Background(colorPanel).
				BorderStyle(lipgloss.ThickBorder()).BorderLeft(true).BorderForeground(colorCyan).Padding(1, 2)
	commandPaletteStyle = lipgloss.NewStyle().Background(colorPanel).
				BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).BorderForeground(colorBorder).Padding(1, 2)
	selectedCommandStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	selectedCommandDetailStyle = lipgloss.NewStyle().Foreground(colorInk)
	disconnectedStyle          = lipgloss.NewStyle().Foreground(colorAmber)
	bootQueryStyle             = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	bootShiftStyle             = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	queuedPromptStyle          = lipgloss.NewStyle().Background(colorPanel).Foreground(colorAmber).
					BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).BorderForeground(colorAmber).Padding(0, 1)
)

