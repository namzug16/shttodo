package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNextReminderTime(t *testing.T) {
	location := time.FixedZone("test", -6*3600)
	cases := []struct {
		now      time.Time
		interval int
		want     time.Time
	}{
		{time.Date(2026, 3, 10, 9, 15, 0, 0, location), 60, time.Date(2026, 3, 10, 10, 0, 0, 0, location)},
		{time.Date(2026, 3, 10, 9, 0, 0, 0, location), 60, time.Date(2026, 3, 10, 10, 0, 0, 0, location)},
		{time.Date(2026, 3, 10, 9, 15, 0, 0, location), 30, time.Date(2026, 3, 10, 9, 30, 0, 0, location)},
		{time.Date(2026, 3, 10, 23, 30, 0, 0, location), 60, time.Date(2026, 3, 11, 0, 0, 0, 0, location)},
	}
	for _, testCase := range cases {
		if got := nextReminderTime(testCase.now, testCase.interval); !got.Equal(testCase.want) {
			t.Errorf("nextReminderTime(%v, %d) = %v, want %v", testCase.now, testCase.interval, got, testCase.want)
		}
	}
}

func TestActiveHours(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 3, 10, hour, 0, 0, 0, time.UTC) }
	if !withinActiveHours(at(9), 9, 21) || !withinActiveHours(at(21), 9, 21) || withinActiveHours(at(22), 9, 21) || withinActiveHours(at(8), 9, 21) {
		t.Error("normal window is wrong")
	}
	if !withinActiveHours(at(23), 22, 6) || !withinActiveHours(at(6), 22, 6) || withinActiveHours(at(12), 22, 6) {
		t.Error("wrapping window is wrong")
	}
	if _, _, err := parseActiveHours("09-21"); err != nil {
		t.Error(err)
	}
	for _, invalid := range []string{"9", "09-24", "a-b", ""} {
		if _, _, err := parseActiveHours(invalid); err == nil {
			t.Errorf("parseActiveHours(%q) accepted invalid input", invalid)
		}
	}
}

func TestBuildReminder(t *testing.T) {
	habits := []Habit{{ID: 1, Title: "Gym"}, {ID: 2, Title: "Read", DoneToday: true}}
	tasks := []Task{
		{ID: 12, Title: "Review PRs", Group: "forte", Priority: "high", Due: "2026-03-09"},
		{ID: 3, Title: "Pay invoice", Group: "master", Priority: "normal", Due: "2026-03-10"},
	}
	text, keyboard := buildReminder(habits, tasks, "2026-03-10")
	want := "Today\n  ⬜ Gym\n\nforte\n  #12 ⚠️ Review PRs (high) due 2026-03-09\n\nmaster\n  #3 📅 Pay invoice due 2026-03-10"
	if text != want {
		t.Errorf("text:\n%s\nwant:\n%s", text, want)
	}
	if len(keyboard) != 3 || keyboard[0][0].CallbackData != "habit:1" || keyboard[1][0].CallbackData != "task:12" {
		t.Errorf("unexpected keyboard: %+v", keyboard)
	}

	if text, keyboard := buildReminder(habits[1:], nil, "2026-03-10"); text != "" || len(keyboard) != 0 {
		t.Errorf("expected empty reminder, got %q %v", text, keyboard)
	}
}

func TestBuildReminderLimits(t *testing.T) {
	var tasks []Task
	for id := 1; id <= 80; id++ {
		tasks = append(tasks, Task{ID: int64(id), Title: strings.Repeat("x", 200), Group: "master"})
	}
	text, keyboard := buildReminder(nil, tasks, "2026-03-10")
	if len(text) > 4096 || len(keyboard) > reminderItemLimit {
		t.Fatalf("over Telegram limits: %d bytes, %d buttons", len(text), len(keyboard))
	}
	if !strings.HasSuffix(text, fmt.Sprintf("+%d more", 80-len(keyboard))) {
		t.Fatalf("missing overflow line: %q", text[len(text)-20:])
	}
}

func TestRunAgent(t *testing.T) {
	output, err := runAgent(t.Context(), "tr a-z A-Z", "pending items")
	if err != nil || output != "PENDING ITEMS" {
		t.Errorf("runAgent = %q, %v", output, err)
	}
	if _, err := runAgent(t.Context(), "echo boom >&2; exit 3", ""); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failing command error = %v", err)
	}
	if _, err := runAgent(t.Context(), "true", ""); err == nil {
		t.Error("empty output must fail")
	}
}
