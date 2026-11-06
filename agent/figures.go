package agent

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"

	ax "github.com/ax-llm/ax/packages/go"
)

// Figure provenance for lead answers.
//
// The lead never sees rows; every figure it reports should trace to something
// it was given or computed. figureLedger records the numbers each source put in
// front of the lead this turn and, once the answer is written, names a source
// for every figure in it:
//
//   - data_agent: a delegated answer this turn. Only the data agent's answer
//     text counts, not its raw rows: the answer is the unit-aware statement
//     ("$16,100"), while a raw row holds 1610000 cents, and a reply quoting that
//     as "$1,610,000" must not look sourced.
//   - conversation: an earlier assistant turn of the thread (restating it).
//   - user: the user's own words, saved memories and skills, the agent's
//     profile, or data attached to the turn.
//   - code: printed or returned by the lead's own code this turn. Combining a
//     few returned figures belongs in code, where the arithmetic is exact.
//
// A figure with no source was produced in the model's head, or invented. The
// check is deterministic and reports only; it never edits the answer.
const (
	FigureSourceDataAgent    = "data_agent"
	FigureSourceConversation = "conversation"
	FigureSourceUser         = "user"
	FigureSourceCode         = "code"

	noticeUnverifiedFigures = "unverified_figures"
)

// Figure is one number in a lead answer and where it came from. An empty
// Source means none of the turn's sources contained it.
type Figure struct {
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
}

// UnverifiedFigures lists the figures in a response that no source contained.
func (r Response) UnverifiedFigures() []string {
	var out []string
	for _, f := range r.Figures {
		if f.Source == "" {
			out = append(out, f.Text)
		}
	}
	return out
}

// figureSourceOrder is the order sources are credited in when several hold the
// same value: a fresh data-agent answer beats a restatement, which beats code.
var figureSourceOrder = []string{FigureSourceDataAgent, FigureSourceConversation, FigureSourceUser, FigureSourceCode}

type figureLedger struct {
	mu     sync.Mutex
	values map[string][]float64
}

func (l *figureLedger) add(source string, texts ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.values == nil {
		l.values = map[string][]float64{}
	}
	for _, text := range texts {
		for _, n := range scanNumbers(text, false) {
			l.values[source] = append(l.values[source], n.value)
			if n.percent {
				// "12.5%" also vouches for the fraction 0.125 and back.
				l.values[source] = append(l.values[source], n.value/100)
			}
		}
	}
}

// addValue records the numbers inside a structured value (a code result).
func (l *figureLedger) addValue(source string, value any) {
	if value == nil {
		return
	}
	l.add(source, stringify(normalizeValue(value)))
}

// attribute names a source for every checkable figure in answer.
func (l *figureLedger) attribute(answer string) []Figure {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Figure
	seen := map[string]bool{}
	for _, n := range scanNumbers(answer, true) {
		if seen[n.text] {
			continue
		}
		seen[n.text] = true
		out = append(out, Figure{Text: n.text, Source: l.sourceOf(n)})
	}
	return out
}

func (l *figureLedger) sourceOf(n scannedNumber) string {
	for _, source := range figureSourceOrder {
		for _, v := range l.values[source] {
			if n.matches(v) {
				return source
			}
		}
	}
	return ""
}

type scannedNumber struct {
	text    string
	value   float64
	unit    float64 // the smallest step the text displays: 0.01 for "4.25", 100 for "16.1k"
	percent bool
}

// matches reports whether a source value, rounded to the precision the figure
// is displayed at, is the figure. Rounding is the only tolerance: no scale
// factors, so a cents value shown as dollars does not match.
func (n scannedNumber) matches(v float64) bool {
	got := math.Abs(v)
	want := math.Abs(n.value)
	if math.Abs(got-want) <= n.unit/2+1e-9*math.Max(1, want) {
		return true
	}
	// A percentage can come from the fraction the source printed.
	return n.percent && math.Abs(got*100-want) <= n.unit/2+1e-9*math.Max(1, want)
}

var (
	figureNumberPattern = regexp.MustCompile(`[-+−]?\$?\d[\d,]*(?:\.\d+)?(?:\s?(?:bn|[kKmMbB])\b)?%?`)
	// Dates, times and markdown list markers carry digits that are not figures.
	figureDatePatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?\b`),
		regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?\b`),
		regexp.MustCompile(`(?i)\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+\d{1,2}(?:st|nd|rd|th)?(?:,?\s+\d{4})?\b`),
		regexp.MustCompile(`(?i)\b\d{1,2}(?:st|nd|rd|th)?\s+(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?(?:,?\s+\d{4})?\b`),
		regexp.MustCompile(`(?m)^\s*\d+[.)]\s`),
	}
)

// scanNumbers finds the numbers in text. For an answer (checkable), it skips
// what is not a reported figure: dates, times, list markers, identifiers such
// as INV-1042 or Q3, years, and bare counts up to ten ("3 accounts", "top 5"),
// which are as often the shape of the reply as a data value.
func scanNumbers(text string, checkable bool) []scannedNumber {
	if text == "" {
		return nil
	}
	// Dates and times are stripped on both sides: in a source their parts
	// would vouch for unrelated figures ("11" from 2026-11-05).
	for _, p := range figureDatePatterns {
		text = p.ReplaceAllStringFunc(text, func(s string) string { return strings.Repeat(" ", len(s)) })
	}
	var out []scannedNumber
	for _, loc := range figureNumberPattern.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		raw := text[start:end]
		if checkable && partOfIdentifier(text, start, end) {
			continue
		}
		n, ok := parseFigure(raw)
		if !ok {
			continue
		}
		if checkable && n.bareInteger() && (math.Abs(n.value) <= 10 || isYear(n)) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (n scannedNumber) bareInteger() bool {
	return !n.percent && n.unit == 1 && !strings.ContainsAny(n.text, "$,")
}

func isYear(n scannedNumber) bool {
	return n.value >= 1900 && n.value <= 2100 && len(strings.TrimLeft(n.text, "+-−")) == 4
}

// partOfIdentifier reports digits glued to letters or code-like punctuation:
// Q3, v2.1, INV-1042, 1/2, item_7.
func partOfIdentifier(text string, start, end int) bool {
	if start > 0 {
		prev := text[start-1]
		if isWordByte(prev) || prev == '.' || prev == '/' || prev == '#' {
			return true
		}
		if prev == '-' && start > 1 && isWordByte(text[start-2]) {
			return true
		}
	}
	if end < len(text) {
		next := text[end]
		if isWordByte(next) {
			return true
		}
		// "1/2" and "3.4.1" are not figures; "$16,100/mo" is.
		if (next == '-' || next == '.' || next == ':' || next == '/') && end+1 < len(text) && isDigit(text[end+1]) {
			return true
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || isDigit(b) || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func parseFigure(raw string) (scannedNumber, bool) {
	n := scannedNumber{text: strings.TrimSpace(raw), unit: 1}
	s := strings.TrimSpace(raw)
	s = strings.TrimLeft(s, "+-−")
	if strings.HasSuffix(s, "%") {
		n.percent = true
		s = strings.TrimSuffix(s, "%")
	}
	s = strings.TrimPrefix(s, "$")
	scale := 1.0
	lower := strings.ToLower(s)
	switch {
	case strings.HasSuffix(lower, "bn"):
		scale, s = 1e9, strings.TrimSpace(s[:len(s)-2])
	case strings.HasSuffix(lower, "k"):
		scale, s = 1e3, strings.TrimSpace(s[:len(s)-1])
	case strings.HasSuffix(lower, "m"):
		scale, s = 1e6, strings.TrimSpace(s[:len(s)-1])
	case strings.HasSuffix(lower, "b"):
		scale, s = 1e9, strings.TrimSpace(s[:len(s)-1])
	}
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return n, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return n, false
	}
	decimals := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		decimals = len(s) - i - 1
	}
	n.value = v * scale
	n.unit = math.Pow(10, -float64(decimals)) * scale
	return n, true
}

// figureRecordingRuntime hands every result the lead's code produces to record, so
// the figures the code printed or returned can vouch for the answer. It wraps
// the runtime without changing what the model sees.
type figureRecordingRuntime struct {
	ax.CodeRuntime
	record func(ax.Value)
}

func (r figureRecordingRuntime) CreateSession(globals map[string]ax.Value, options map[string]ax.Value) (ax.CodeSession, error) {
	session, err := r.CodeRuntime.CreateSession(globals, options)
	if err != nil || session == nil {
		return session, err
	}
	return figureRecordingSession{CodeSession: session, record: r.record}, nil
}

type figureRecordingSession struct {
	ax.CodeSession
	record func(ax.Value)
}

func (s figureRecordingSession) Execute(code string, options map[string]ax.Value) ax.Value {
	result := s.CodeSession.Execute(code, options)
	s.record(result)
	return result
}

// recordCodeResult keeps what a code step printed (its console output) and the
// value it evaluated to. A final() completion is the answer itself, so it never
// vouches for the answer.
func (l *figureLedger) recordCodeResult(result ax.Value) {
	payload, ok := normalizeValue(result).(map[string]any)
	if !ok {
		return
	}
	for _, line := range anySlice(payload["logs"]) {
		l.add(FigureSourceCode, stringValue(line))
	}
	if stringValue(payload["kind"]) == "result" {
		l.addValue(FigureSourceCode, payload["result"])
	}
}
