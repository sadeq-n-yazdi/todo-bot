package web

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// assignToAnn adds an item of the group, made by Bob and given to Ann.
func (e *writeEnv) assignToAnn(text string) itemDTO {
	e.t.Helper()
	w := e.do("POST", chatPath(groupFamily, "/items"), fmt.Sprintf(`{"text":%q,"assignee_id":%d}`, text, idAnn), bearer(e.bob))
	wantStatus(e.t, w, 201)
	return decodeBody[itemDTO](e.t, w)
}

func TestDidNoticeOneTimeInCreatorLocale(t *testing.T) {
	e := newWriteEnv(t)
	it := e.assignToAnn("fix the <door>")
	e.send.Reset()
	// Ann (fa) completes it from her private chat; Bob, the creator, is en.
	p := chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID))
	wantStatus(t, e.do("POST", p, `{}`, bearer(e.ann)), 200)
	wantErr(t, e.do("POST", p, `{}`, bearer(e.ann)), 404, "not_found")
	msgs := e.send.Messages()
	if len(msgs) != 1 {
		t.Fatalf("%d messages, want 1: %+v", len(msgs), msgs)
	}
	m := msgs[0]
	want := fmt.Sprintf(`✅ <a href="tg://user?id=%d">@ann</a> did: fix the &lt;door&gt;`, idAnn)
	if m.ChatID != groupFamily || m.Msg.Text != want {
		t.Fatalf("notice to %d: %q, want %q", m.ChatID, m.Msg.Text, want)
	}
}

func TestDidNoticeFarsiCreator(t *testing.T) {
	e := newWriteEnv(t)
	e.setUser(idBob, i18n.Fa, calendar.Jalali, "UTC")
	it := e.assignToAnn("fix the door")
	e.send.Reset()
	wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann)), 200)
	msgs := e.send.Messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Msg.Text, "انجام داد") {
		t.Fatalf("messages %+v", msgs)
	}
}

func TestDidNoticeOnlyForAssigneeFromAnotherChat(t *testing.T) {
	e := newWriteEnv(t)
	// The assignee completes the item in the group: no notice.
	it := e.assignToAnn("one")
	e.send.Reset()
	wantStatus(t, e.do("POST", chatPath(groupFamily, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann)), 200)
	// The creator completes an item of Ann in the group: no notice.
	it = e.assignToAnn("two")
	wantStatus(t, e.do("POST", chatPath(groupFamily, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.bob)), 200)
	// An item with no assignee: no notice.
	it = e.openItem(e.bob, groupFamily, "three")
	wantStatus(t, e.do("POST", chatPath(groupFamily, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann)), 200)
	// An item of the private chat of Bob, given to Ann, done by Bob.
	w := e.do("POST", chatPath(idBob, "/items"), fmt.Sprintf(`{"text":"four","assignee_id":%d}`, idAnn), bearer(e.bob))
	wantStatus(t, w, 201)
	it = decodeBody[itemDTO](t, w)
	wantStatus(t, e.do("POST", chatPath(idBob, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.bob)), 200)
	if msgs := e.send.Messages(); len(msgs) != 0 {
		t.Fatalf("notices %+v", msgs)
	}
}

// A repeating item due more than 12 hours later does not change on Done,
// so the server sends no notice.
func TestDidNoticeNotForUnchangedRepeat(t *testing.T) {
	e := newWriteEnv(t)
	td := e.addTodo(model.Todo{ChatID: groupFamily, Text: "weekly", DueAt: at(2026, 3, 5, 9, 0),
		Recur: "weekly thu 09:00", TZ: "UTC", AssigneeID: idAnn, CreatedBy: idBob})
	w := e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", td.ID)), `{}`, bearer(e.ann))
	wantStatus(t, w, 200)
	if resp := decodeBody[doneResponse](t, w); !resp.StillOpen {
		t.Fatalf("done %+v", resp)
	}
	if msgs := e.send.Messages(); len(msgs) != 0 {
		t.Fatalf("notices %+v", msgs)
	}
}

func TestDidNoticeSendErrorKeeps200(t *testing.T) {
	e := newWriteEnv(t)
	it := e.assignToAnn("fix the door")
	e.send.ErrFn = func(int64, tg.OutMessage) error { return errors.New("telegram is down") }
	w := e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann))
	wantStatus(t, w, 200)
	if !strings.Contains(e.log.String(), "did notice") {
		t.Errorf("no warning in the log:\n%s", e.log)
	}
}

func TestEchoText(t *testing.T) {
	long := strings.Repeat("ب", didEchoRunes+5)
	if got := echoText(long); got != strings.Repeat("ب", didEchoRunes)+"…" {
		t.Errorf("long text: %q", got)
	}
	if got := echoText("a & b"); got != "a &amp; b" {
		t.Errorf("escape: %q", got)
	}
}
