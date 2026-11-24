package app

import "testing"

func TestPlayedReceiptIdempotent(t *testing.T) {
	f := newFixture(t)
	f.start()
	sid := str(f.a.current(), "id")
	_, err := f.a.db.Exec(`INSERT INTO host_topics VALUES('t','a1','k','title','[{"id":"x","text":"angle"}]','ready',1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.a.RegisterPlayback(sid, "h", "host", "t", "x"); err != nil {
		t.Fatal(err)
	}
	r := TaskResult{Seq: 1, TaskID: "h", State: "completed", Text: "spoken", CompletedAt: f.a.c.Clock()}
	for i := 0; i < 2; i++ {
		if err = f.a.RecordPlayed(sid, r); err != nil {
			t.Fatal(err)
		}
	}
	in, err := f.a.LoadHostContext("a1", sid)
	if err != nil || len(in.SpokenTurns) != 1 || in.CurrentAngleID != "x" || in.CurrentTopic.State != "done" {
		t.Fatal(in, err)
	}
}
func TestMissingReceiptDoesNotAdvanceTopic(t *testing.T) {
	f := newFixture(t)
	f.start()
	sid := str(f.a.current(), "id")
	if err := f.a.RegisterPlayback(sid, "lost", "manual", "", ""); err != nil {
		t.Fatal(err)
	}
	page := TaskResultsPage{Results: []TaskResult{{Seq: 3, TaskID: "else", State: "failed"}}, NextSeq: 3, Truncated: true}
	if err := f.a.ApplyTaskResults(sid, page); err != nil {
		t.Fatal(err)
	}
	h, err := New(f.a.c)
	if err != nil {
		t.Fatal(err)
	}
	a := h.(*App)
	defer a.Close()
	seq, err := a.PlaybackCursor(sid)
	if err != nil || seq != 3 {
		t.Fatal(seq, err)
	}
	var state string
	if err = a.db.QueryRow("SELECT state FROM host_tasks WHERE session_id=? AND task_id='lost'", sid).Scan(&state); err != nil || state != "unknown" {
		t.Fatal(state, err)
	}
	in, err := a.LoadHostContext("a1", sid)
	if err != nil || len(in.SpokenTurns) != 0 || in.CurrentAngleID != "" {
		t.Fatal(in, err)
	}
}
