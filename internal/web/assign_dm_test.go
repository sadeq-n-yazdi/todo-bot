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

func TestShouldNotifyAssigned(t *testing.T) {
	for _, c := range []struct {
		old, now, editor int64
		want             bool
	}{
		{0, idBob, idAnn, true},      // a new assignee
		{777, idBob, idAnn, true},    // another assignee
		{idBob, idBob, idAnn, false}, // unchanged
		{idBob, 0, idAnn, false},     // cleared
		{0, idAnn, idAnn, false},     // the editor
	} {
		if got := shouldNotifyAssigned(c.old, model.Todo{AssigneeID: c.now}, c.editor); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestAssignedDMOnceInAssigneeLanguage(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "fix the <door> tomorrow 10:00")
	e.send.Reset()
	got := e.patch(e.ann, idAnn, it.ID, fmt.Sprintf(`{"assignee_id":%d}`, idBob))
	if got.Assignee == nil || got.Assignee.ID != idBob || got.Note != "" {
		t.Fatalf("item %+v", got)
	}
	msgs := e.send.Messages()
	if len(msgs) != 1 {
		t.Fatalf("%d messages, want 1: %+v", len(msgs), msgs)
	}
	m := msgs[0]
	// Bob is en and UTC: 10:00 in Tehran is 06:30 UTC.
	want := fmt.Sprintf(`<a href="tg://user?id=%d">@ann</a> assigned you an item: fix the &lt;door&gt; — `, idAnn)
	if m.ChatID != idBob || !strings.HasPrefix(m.Msg.Text, want) || !strings.Contains(m.Msg.Text, "06:30") {
		t.Fatalf("message to %d: %q, want the prefix %q", m.ChatID, m.Msg.Text, want)
	}
	if strings.Contains(m.Msg.Text, "→") || len(m.Msg.Keyboard) != 0 {
		t.Errorf("message %+v: no assignee suffix and no buttons", m.Msg)
	}

	// The same assignee again, a text change, and a cleared assignee: no
	// message.
	e.send.Reset()
	e.patch(e.ann, idAnn, it.ID, fmt.Sprintf(`{"assignee_id":%d}`, idBob))
	e.patch(e.ann, idAnn, it.ID, `{"text":"fix the door now"}`)
	e.patch(e.ann, idAnn, it.ID, `{"assignee_id":0}`)
	if msgs := e.send.Messages(); len(msgs) != 0 {
		t.Fatalf("messages %+v", msgs)
	}

	// A Farsi assignee with the Jalali calendar gets Farsi.
	e.setUser(idBob, i18n.Fa, calendar.Jalali, "UTC")
	e.patch(e.ann, idAnn, it.ID, fmt.Sprintf(`{"assignee_id":%d,"text":"fix the door"}`, idBob))
	msgs = e.send.Messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Msg.Text, "محول کرد") || !strings.Contains(msgs[0].Msg.Text, "۰۶:۳۰") {
		t.Fatalf("messages %+v", msgs)
	}
}

// The old assignee is read before the save: a group item made by Bob and
// given to Ann, which Ann gives back to Bob, sends him one message.
func TestAssignedDMUsesOldAssignee(t *testing.T) {
	e := newWriteEnv(t)
	it := e.assignToAnn("water the garden")
	e.send.Reset()
	got := e.patch(e.ann, groupFamily, it.ID, fmt.Sprintf(`{"assignee_id":%d}`, idBob))
	if got.Assignee == nil || got.Assignee.ID != idBob {
		t.Fatalf("item %+v", got)
	}
	if msgs := e.send.Messages(); len(msgs) != 1 || msgs[0].ChatID != idBob {
		t.Fatalf("messages %+v", msgs)
	}
}

func TestAssignedDMBlockedStillSaves(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "call the plumber")
	e.send.ErrFn = func(chatID int64, _ tg.OutMessage) error {
		return &tg.APIError{Code: 403, Desc: "Forbidden: bot was blocked by the user"}
	}
	w := e.do("PATCH", itemPath(idAnn, it.ID), fmt.Sprintf(`{"assignee_id":%d}`, idBob), bearer(e.ann))
	wantStatus(t, w, 200)
	got := decodeBody[editItemDTO](t, w)
	if got.Assignee == nil || got.Assignee.ID != idBob {
		t.Fatalf("item %+v", got)
	}
	if e.todo(it.ID).AssigneeID != idBob {
		t.Fatal("the save did not happen")
	}
	// The note is plain text in the language of Ann (fa).
	want := (i18n.Locale{Lang: i18n.Fa}).T("web.edit.nodm", "@bob")
	if got.Note != want || strings.Contains(got.Note, "<") {
		t.Errorf("note %q, want %q", got.Note, want)
	}
	if !strings.Contains(e.log.String(), "assigned dm not delivered") {
		t.Errorf("log %s", e.log.String())
	}
}

func TestAssignedDMOtherErrorNoNote(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "call the plumber")
	e.send.ErrFn = func(int64, tg.OutMessage) error { return errors.New("network down") }
	got := e.patch(e.ann, idAnn, it.ID, fmt.Sprintf(`{"assignee_id":%d}`, idBob))
	if got.Note != "" || got.Assignee == nil || got.Assignee.ID != idBob {
		t.Fatalf("item %+v", got)
	}
	if log := e.log.String(); !strings.Contains(log, "web: assigned dm") || !strings.Contains(log, "level=WARN") {
		t.Errorf("log %s", log)
	}
	if n := len(e.send.Messages()); n != 0 {
		t.Errorf("%d messages recorded", n)
	}
}

func TestEditStringsBothLanguages(t *testing.T) {
	for _, k := range i18n.Keys(i18n.En) {
		if !strings.HasPrefix(k, "web.edit.") && k != "web.tg.assigned" {
			continue
		}
		if !hasKey(i18n.Fa, k) {
			t.Errorf("%s has no Farsi string", k)
		}
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			s := (i18n.Locale{Lang: lang}).T(k)
			if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(s, "%s", ""), "%d", ""), "%") {
				t.Errorf("%s (%s) uses a verb other than %%s and %%d: %q", k, lang, s)
			}
		}
	}
	if !hasKey(i18n.Fa, "web.tg.assigned") || !hasKey(i18n.En, "web.edit.nodm") {
		t.Error("missing keys")
	}
}
