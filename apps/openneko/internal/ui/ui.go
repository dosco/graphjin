// Package ui holds the shared look-and-feel for the interactive `openneko
// setup` flow: lipgloss styles, the huh form theme, a spinner wrapper for
// silent operations, and a few framed printers. It deliberately owns no flow
// logic — callers compose forms and call these for chrome.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	accent = lipgloss.Color("99") // OpenNeko purple
	green  = lipgloss.Color("42")
	red    = lipgloss.Color("203")
	dim    = lipgloss.Color("241")
)

var (
	brand    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	boldText = lipgloss.NewStyle().Bold(true)
	dimText  = lipgloss.NewStyle().Foreground(dim)
	okText   = lipgloss.NewStyle().Foreground(green)
	errText  = lipgloss.NewStyle().Foreground(red)
	stepTag  = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

// Theme is the huh theme every form in the setup flow uses.
func Theme() *huh.Theme { return huh.ThemeCharm() }

// Banner prints the setup header.
func Banner(w io.Writer, subtitle string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, brand.Render("⬢ OpenNeko setup"))
	if subtitle != "" {
		fmt.Fprintln(w, dimText.Render("  "+subtitle))
	}
}

// StepHeader prints a numbered step heading with a one-line description.
func StepHeader(w io.Writer, n, total int, title, desc string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, stepTag.Render(fmt.Sprintf("Step %d/%d", n, total))+"  "+boldText.Render(title))
	if desc != "" {
		fmt.Fprintln(w, dimText.Render("  "+desc))
	}
}

// Success / Failure / Info print a single status line.
func Success(w io.Writer, format string, a ...any) {
	fmt.Fprintln(w, okText.Render("  ✓ ")+fmt.Sprintf(format, a...))
}

func Failure(w io.Writer, format string, a ...any) {
	fmt.Fprintln(w, errText.Render("  ✗ ")+fmt.Sprintf(format, a...))
}

func Info(w io.Writer, format string, a ...any) {
	fmt.Fprintln(w, dimText.Render("  "+fmt.Sprintf(format, a...)))
}

// Spin shows a spinner with title while action runs, returning action's error.
// Without a TTY (CI, piped output) it just runs the action — no animation — so
// it's safe on the headless path too.
func Spin(title string, action func() error) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return action()
	}
	var actErr error
	if err := spinner.New().
		Title(" " + title).
		Type(spinner.Dots).
		Action(func() { actErr = action() }).
		Run(); err != nil {
		return err
	}
	return actErr
}

// CompletionBox renders a rounded box around the given lines (used for the
// final "what next" summary).
func CompletionBox(w io.Writer, lines ...string) {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2)
	fmt.Fprintln(w)
	fmt.Fprintln(w, box.Render(strings.Join(lines, "\n")))
}

// Heading returns bold accent text (for inline emphasis).
func Heading(s string) string { return brand.Render(s) }

// OK / Bad return a styled check / cross glyph for inline use.
func OK() string  { return okText.Render("✓") }
func Bad() string { return errText.Render("✗") }
