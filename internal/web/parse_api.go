package web

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// maxItemRunes is the limit of the text of an item, the same as
// maxTodoChars of the handler (docs/WEB-DESIGN.md §5.4, §11.3).
const maxItemRunes = 1000

type parseRequest struct {
	Text string `json:"text"`
}

// parseResponse is the preview of endpoint 15. The due fields are null
// when the text has no date; recur and recur_text are "" when it does not
// repeat.
type parseResponse struct {
	Text      string     `json:"text"`
	DueAt     *time.Time `json:"due_at"`
	DueText   *string    `json:"due_text"`
	Recur     string     `json:"recur"`
	RecurText string     `json:"recur_text"`
}

// itemText applies the first two steps of §5.4 to the text of an item. It
// counts the runes of the text after i18n.NormalizeDigits and
// strings.TrimSpace, and refuses an empty text (empty_text) and a text of
// more than maxItemRunes runes (too_long). It returns the raw text, only
// trimmed: the item keeps the digits as the user wrote them, as /add does.
// The parser normalises the digits itself to find the date.
func itemText(raw string) (string, error) {
	norm := strings.TrimSpace(i18n.NormalizeDigits(raw))
	switch {
	case norm == "":
		return "", errEmptyText
	case utf8.RuneCountInString(norm) > maxItemRunes:
		return "", errTooLong
	}
	return strings.TrimSpace(raw), nil
}

// parseItem parses text in the time zone and the calendar of the user of
// r, as /add does (§5.4 step 3).
func (s *Server) parseItem(r *http.Request, text string) (nlparse.Result, error) {
	v := s.viewOf(r)
	return s.d.Parser.Parse(text, v.now.In(v.loc), v.lc.Cal)
}

// handleParse is endpoint 15, POST /api/parse. It gives the live preview of
// the Add form: the text without the date phrase, the date and the repeat.
// It writes nothing.
func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	text, err := itemText(req.Text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.parseItem(r, text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.viewOf(r)
	out := parseResponse{Text: res.Text}
	due := res.Due
	if res.Rule != nil {
		out.Recur = res.Rule.String()
		out.RecurText = ui.DescribeRule(out.Recur, v.lc)
		// The service stores the next occurrence when the first one is not
		// in the future; the preview shows the same date.
		if due == nil || !due.After(v.now) {
			if next := res.Rule.Next(v.now, v.loc); !next.IsZero() {
				due = &next
			}
		}
	}
	if due != nil {
		at := due.UTC()
		txt := ui.FormatWhen(at, v.lc, v.loc, v.now)
		out.DueAt, out.DueText = &at, &txt
	}
	writeJSON(w, http.StatusOK, out)
}
