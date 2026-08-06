package ui

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Background string
	Foreground string
	Accent     string
	Error      string
	Muted      string
	Selected   string
	Border     string
	Warning    string
	Info       string
}

func DefaultTheme() Theme {
	return Theme{
		Background: "#2b3339",
		Foreground: "#d3c6aa",
		Accent:     "#a7c080",
		Error:      "#e67e80",
		Muted:      "#859289",
		Selected:   "#475258",
		Border:     "#4f585e",
		Warning:    "#dbbc7f",
		Info:       "#0a84ff",
	}
}

type Styles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	Muted        lipgloss.Style
	Accent       lipgloss.Style
	Warning      lipgloss.Style
	Error        lipgloss.Style
	ErrorBanner  lipgloss.Style
	Selected     lipgloss.Style
	Border       lipgloss.Style
	Status       lipgloss.Style
	Incoming     lipgloss.Style
	Outgoing     lipgloss.Style
	Sender       lipgloss.Style
	Timestamp    lipgloss.Style
	FilterPrompt lipgloss.Style
}

func NewStyles(theme Theme) Styles {
	background := lipgloss.Color(theme.Background)
	foreground := lipgloss.Color(theme.Foreground)
	accent := lipgloss.Color(theme.Accent)
	muted := lipgloss.Color(theme.Muted)
	selected := lipgloss.Color(theme.Selected)
	border := lipgloss.Color(theme.Border)
	errorColor := lipgloss.Color(theme.Error)
	warning := lipgloss.Color(theme.Warning)
	info := lipgloss.Color(theme.Info)
	incoming := lipgloss.Color("#6b7378")

	return Styles{
		Title:       lipgloss.NewStyle().Bold(true).Foreground(accent),
		Subtitle:    lipgloss.NewStyle().Bold(true).Foreground(foreground),
		Muted:       lipgloss.NewStyle().Foreground(muted),
		Accent:      lipgloss.NewStyle().Foreground(accent),
		Warning:     lipgloss.NewStyle().Foreground(warning),
		Error:       lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		ErrorBanner: lipgloss.NewStyle().Background(errorColor).Foreground(background).Bold(true).Padding(0, 1),
		Selected:    lipgloss.NewStyle().Background(selected).Foreground(foreground).Bold(true),
		Border:      lipgloss.NewStyle().Foreground(border),
		Status:      lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		Incoming: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(incoming).
			Foreground(foreground).
			Padding(0, 1),
		Outgoing: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(info).
			Foreground(foreground).
			Padding(0, 1),
		Sender:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		Timestamp:    lipgloss.NewStyle().Foreground(muted),
		FilterPrompt: lipgloss.NewStyle().Foreground(accent).Bold(true),
	}
}

func DefaultStyles() Styles { return NewStyles(DefaultTheme()) }
