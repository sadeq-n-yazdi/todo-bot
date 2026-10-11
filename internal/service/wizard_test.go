package service

import (
	"errors"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Wizard drafts (#68).

func TestDraftLifecycle(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	d := model.Draft{ChatID: groupChat, UserID: 7, MsgID: 11, Step: model.StepText}
	e.must(e.svc.StartDraft(ctx, &d))
	if !d.UpdatedAt.Equal(t0) || !d.ExpiresAt.Equal(t0.Add(30*time.Minute)) || d.Version == 0 {
		t.Fatalf("StartDraft set %+v", d)
	}

	// Node B continues it: one press wins, the same stale press loses.
	e.clk.Advance(10 * time.Minute)
	got, err := b.DraftByMessage(ctx, groupChat, 11)
	e.must(err)
	stale := got
	got.Step, got.Data.Text = model.StepDate, "buy milk"
	e.must(b.UpdateDraft(ctx, &got))
	if got.Version == stale.Version || !got.ExpiresAt.Equal(e.clk.Now().Add(30*time.Minute)) {
		t.Fatalf("UpdateDraft left %+v", got)
	}
	before := stale
	if err := e.svc.UpdateDraft(ctx, &stale); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("stale UpdateDraft: %v, want ErrStaleDraft", err)
	}
	if stale != before {
		t.Fatal("a failed UpdateDraft changed its argument")
	}
	if cur, err := e.svc.Draft(ctx, groupChat, 7); err != nil || cur.Data.Text != "buy milk" {
		t.Fatalf("Draft = %+v, %v", cur, err)
	}

	// Inactivity: 30 minutes after the last change it is gone.
	e.clk.Advance(30 * time.Minute)
	if _, err := e.svc.Draft(ctx, groupChat, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired Draft: %v, want ErrNotFound", err)
	}
	if err := e.svc.UpdateDraft(ctx, &got); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("UpdateDraft of an expired draft: %v, want ErrStaleDraft", err)
	}
}

func TestEndDraftOnce(t *testing.T) {
	e := newEnv(t)
	d := model.Draft{ChatID: 7, UserID: 7, MsgID: 3, Step: model.StepConfirm}
	e.must(e.svc.StartDraft(ctx, &d))
	e.must(e.svc.EndDraft(ctx, d))
	if err := e.svc.EndDraft(ctx, d); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("second EndDraft: %v, want ErrStaleDraft", err)
	}
	if _, err := e.svc.Draft(ctx, 7, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ended draft: %v", err)
	}
}

func TestNamedUsers(t *testing.T) {
	e := newEnv(t)
	e.must(e.note(7, "alice"))
	e.must(e.note(8, "Bob_X"))
	_, err := e.svc.Prefs(ctx, 9, "") // carol: no username
	e.must(err)
	us, err := e.svc.NamedUsers(ctx, 7)
	e.must(err)
	if len(us) != 1 || us[0].ID != 8 || us[0].Username != "bob_x" {
		t.Fatalf("NamedUsers = %+v", us)
	}
}

func TestKBVersion(t *testing.T) {
	e := newEnv(t)
	// A user the bot has not seen yet is created.
	e.must(e.svc.SetKBVersion(ctx, 7, 1))
	p, err := e.svc.Prefs(ctx, 7, "")
	e.must(err)
	if p.KBVersion != 1 {
		t.Fatalf("Prefs.KBVersion = %d, want 1", p.KBVersion)
	}
	// Changing the language keeps it; the handler re-sends on /lang itself.
	e.must(e.svc.SetLang(ctx, 7, "fa"))
	p, err = e.svc.Prefs(ctx, 7, "")
	e.must(err)
	if p.KBVersion != 1 {
		t.Fatalf("after SetLang KBVersion = %d, want 1", p.KBVersion)
	}
	if p, err := e.svc.Prefs(ctx, 8, ""); err != nil || p.KBVersion != 0 {
		t.Fatalf("new user Prefs = %+v, %v", p, err)
	}
}
