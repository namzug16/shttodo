package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseTaskMessage(t *testing.T) {
	wednesday := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	valueOf := func(value *string) string {
		if value == nil {
			return "<nil>"
		}
		return *value
	}
	cases := []struct {
		message                                     string
		wantTitle, wantGroup, wantPriority, wantDue string
		wantNotes                                   string
	}{
		{"Buy milk", "Buy milk", "<nil>", "<nil>", "<nil>", "<nil>"},
		{"Review PRs #work !high @tomorrow", "Review PRs", "work", "high", "2026-11-26", "<nil>"},
		{"Fix #1 priority bug #work", "Fix #1 priority bug", "work", "<nil>", "<nil>", "<nil>"},
		{"Call mom @wed", "Call mom", "<nil>", "<nil>", "2026-12-02", "<nil>"},
		{"Pay rent @fri !low", "Pay rent", "<nil>", "low", "2026-11-27", "<nil>"},
		{"Ship it @2026-12-24\nask finance first\nthen deploy", "Ship it", "<nil>", "<nil>", "2026-12-24", "ask finance first\nthen deploy"},
		{"#work", "#work", "<nil>", "<nil>", "<nil>", "<nil>"},
	}
	for _, testCase := range cases {
		fields, err := parseTaskMessage(testCase.message, wednesday)
		if err != nil {
			t.Errorf("%q: %v", testCase.message, err)
			continue
		}
		got := []string{valueOf(fields.Title), valueOf(fields.Group), valueOf(fields.Priority), valueOf(fields.Due), valueOf(fields.Notes)}
		want := []string{testCase.wantTitle, testCase.wantGroup, testCase.wantPriority, testCase.wantDue, testCase.wantNotes}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%q: got %q, want %q", testCase.message, got, want)
		}
	}

	for _, invalid := range []string{"x !urgent", "x @someday", "x #a #b", "x @2026-13-01"} {
		if _, err := parseTaskMessage(invalid, wednesday); err == nil {
			t.Errorf("%q: expected an error", invalid)
		}
	}
}

func TestHandleTextMessage(t *testing.T) {
	r := &reminder{store: newTestStore(t)}
	if _, err := r.store.CreateGroup("work"); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ message, wantReply string }{
		{"Buy milk", "Added #1 Buy milk (master)"},
		{"/add@shttodo_bot Review #work", "Added #2 Review (work)"},
		{"/add Plan !high #nope", `Error: group "nope" does not exist. Groups: master, work`},
		{"/habit Gym\nevery morning", "Added habit #1 Gym"},
		{"/add", "Error: the title is empty"},
		{"/bogus", "Unknown command /bogus. Send /help."},
	}
	for _, testCase := range cases {
		if got := r.handleTextMessage(t.Context(), testCase.message); got != testCase.wantReply {
			t.Errorf("%q: got %q, want %q", testCase.message, got, testCase.wantReply)
		}
	}
	habit, _ := r.store.GetHabit(1, "")
	if habit.Notes != "every morning" {
		t.Errorf("habit notes: got %q", habit.Notes)
	}
}
