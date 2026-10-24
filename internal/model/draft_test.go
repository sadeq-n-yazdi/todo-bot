package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDraftDataEditFieldsRoundTrip(t *testing.T) {
	base := &DraftData{Text: "milk", OrigText: "milk", Date: "2026-10-30", Repeat: RepeatWeekly}
	in := DraftData{Text: "milk 2", Date: "2026-10-31", EditTodoID: 42, EditNum: 3, EditBase: base}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out DraftData
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip: got %+v, want %+v", out, in)
	}
}

func TestDraftDataOldJSON(t *testing.T) {
	old := `{"text":"milk","date":"2026-10-30","repeat":"daily","assignee":7}`
	var d DraftData
	if err := json.Unmarshal([]byte(old), &d); err != nil {
		t.Fatal(err)
	}
	if d.EditTodoID != 0 || d.EditNum != 0 || d.EditBase != nil {
		t.Fatalf("new fields not zero: %+v", d)
	}
	// omitempty keeps an old draft byte-identical.
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != old {
		t.Fatalf("re-encoded %s, want %s", b, old)
	}
}
