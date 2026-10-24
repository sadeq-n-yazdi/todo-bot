package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func editView(d model.DraftData) WizardView {
	v := wview(model.StepConfirm, d)
	v.EditNum = 3
	return v
}

func TestWizardEditCard(t *testing.T) {
	due := time.Date(2026, 10, 14, 15, 0, 0, 0, testLoc).UTC()
	plain := model.DraftData{Text: "buy <milk>", Date: "2026-10-14", Time: "15:00", Repeat: model.RepeatWeekly}
	found := func(rule string) model.DraftData {
		return model.DraftData{Text: "pay <rent>", FoundDue: &due, FoundRule: rule, FoundTime: true}
	}
	cases := []struct {
		name string
		d    model.DraftData
	}{
		{"plain", plain},
		{"found_every3d", found("every 3d 2026-10-14 15:00")},
		{"found_weekly", found("weekly mon,wed 15:00")},
		{"found_jalali_monthly", found("monthly 22 15:00 jalali")},
	}
	locs := []struct {
		name string
		lc   i18n.Locale
	}{
		{"en_gregorian", locEnG},
		{"en_jalali", i18n.Locale{Lang: i18n.En, Cal: calendar.Jalali}},
		{"fa_gregorian", i18n.Locale{Lang: i18n.Fa, Cal: calendar.Gregorian}},
		{"fa_jalali", locFaJ},
	}
	for _, l := range locs {
		for _, c := range cases {
			t.Run(l.name+"/"+c.name, func(t *testing.T) {
				m := WizardCard(editView(c.d), l.lc, testLoc, testNow)
				for _, b := range buttonsOf(m) {
					if len(b.Data) > 64 {
						t.Errorf("data %q is %d bytes", b.Data, len(b.Data))
					}
					if _, err := Decode(b.Data); err != nil {
						t.Errorf("data %q: %v", b.Data, err)
					}
				}
				for _, arg := range []string{WizNavSave, WizNavEdit, WizNavCancel} {
					if !hasData(m, nav(arg)) {
						t.Errorf("no nav %q", arg)
					}
				}
				if strings.Contains(m.Text, "<") {
					t.Errorf("unescaped text %q", m.Text)
				}
				if l.lc.Lang == i18n.Fa && !strings.Contains(m.Text, "۳") {
					t.Errorf("no Persian digit in header: %q", m.Text)
				}
				golden(t, "wizard_card_edit_"+c.name+"_"+l.name+".golden", dump(m))
			})
		}
	}
}

func TestWizardEditFoundRuleIsDescribed(t *testing.T) {
	due := time.Date(2026, 10, 14, 15, 0, 0, 0, testLoc).UTC()
	for _, c := range []struct{ rule, en string }{
		{"every 3d 2026-10-14 15:00", "every 3 days"},
		{"weekly mon,wed 15:00", "every Mon, Wed"},
		{"monthly 22 15:00 jalali", "every Jalali month on the 22nd"},
	} {
		v := editView(model.DraftData{Text: "x", FoundDue: &due, FoundRule: c.rule, FoundTime: true})
		m := WizardCard(v, locEnG, testLoc, testNow)
		if !strings.Contains(m.Text, "🔁 "+c.en) {
			t.Errorf("%s: %q", c.rule, m.Text)
		}
		if strings.Contains(m.Text, "from your text") {
			t.Errorf("%s: generic label: %q", c.rule, m.Text)
		}
	}
	// A rule that cannot be described falls back to the generic label.
	v := editView(model.DraftData{Text: "x", FoundDue: &due, FoundRule: "bogus"})
	if m := WizardCard(v, locEnG, testLoc, testNow); !strings.Contains(m.Text, "Repeats (from your text)") {
		t.Errorf("fallback: %q", m.Text)
	}
}

func TestWizardEditLabelsAndHeader(t *testing.T) {
	d := model.DraftData{Text: "x", Date: "2026-10-14"}
	en := WizardCard(editView(d), locEnG, testLoc, testNow)
	if !strings.HasPrefix(en.Text, "✏️ Editing item 3\n\n") || !strings.Contains(dump(en), "[✅ Save changes|") {
		t.Errorf("en: %s", dump(en))
	}
	fa := WizardCard(editView(d), locFaJ, testLoc, testNow)
	if !strings.Contains(fa.Text, "۳") || !strings.Contains(dump(fa), "ذخیره تغییرات") {
		t.Errorf("fa: %s", dump(fa))
	}
}

func TestWizardCreateModeUnchanged(t *testing.T) {
	d := model.DraftData{Text: "x", Date: "2026-10-14"}
	m := WizardCard(wview(model.StepConfirm, d), locEnG, testLoc, testNow)
	if strings.Contains(m.Text, "Editing") || strings.Contains(dump(m), "Save changes") || !strings.Contains(dump(m), "[✅ Save|") {
		t.Errorf("create card changed: %s", dump(m))
	}
	// A found rule in create mode keeps the generic label.
	due := time.Date(2026, 10, 14, 15, 0, 0, 0, testLoc).UTC()
	m = WizardCard(wview(model.StepConfirm, model.DraftData{Text: "x", FoundDue: &due, FoundRule: "every 3d 2026-10-14 15:00"}), locEnG, testLoc, testNow)
	if !strings.Contains(m.Text, "Repeats (from your text)") {
		t.Errorf("create found: %q", m.Text)
	}
}
