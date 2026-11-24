package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func stringPointer(value string) *string { return &value }

func requireErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.Code != wantCode {
		t.Fatalf("want error code %q, got %v", wantCode, err)
	}
}

func TestCalculateStreaks(t *testing.T) {
	cases := []struct {
		name                     string
		dates                    []string
		today                    string
		wantCurrent, wantLongest int
	}{
		{"empty", nil, "2026-03-10", 0, 0},
		{"done today", []string{"2026-03-08", "2026-03-09", "2026-03-10"}, "2026-03-10", 3, 3},
		{"alive until today ends", []string{"2026-03-08", "2026-03-09"}, "2026-03-10", 2, 2},
		{"broken", []string{"2026-03-01", "2026-03-02", "2026-03-03", "2026-03-08"}, "2026-03-10", 0, 3},
		{"across month end", []string{"2026-02-27", "2026-02-28", "2026-03-01"}, "2026-03-01", 3, 3},
	}
	for _, testCase := range cases {
		current, longest := calculateStreaks(testCase.dates, testCase.today)
		if current != testCase.wantCurrent || longest != testCase.wantLongest {
			t.Errorf("%s: got current=%d longest=%d, want %d %d", testCase.name, current, longest, testCase.wantCurrent, testCase.wantLongest)
		}
	}
}

func TestListTasksOrder(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateGroup("work"); err != nil {
		t.Fatal(err)
	}
	create := func(title, group, priority, due string) {
		if _, err := store.CreateTask(TaskFields{Title: &title, Group: &group, Priority: &priority, Due: &due}); err != nil {
			t.Fatal(err)
		}
	}
	create("no due", "master", "normal", "")
	create("later", "master", "normal", "2026-12-01")
	create("sooner", "master", "normal", "2026-11-01")
	create("high", "master", "high", "")
	create("low", "master", "low", "2026-01-01")
	create("work", "work", "normal", "")

	tasks, err := store.ListTasks("", false)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, task := range tasks {
		titles = append(titles, task.Title)
	}
	want := []string{"high", "sooner", "later", "no due", "low", "work"}
	if len(titles) != len(want) {
		t.Fatalf("got %v, want %v", titles, want)
	}
	for index := range want {
		if titles[index] != want[index] {
			t.Fatalf("got %v, want %v", titles, want)
		}
	}
}

func TestGroupRules(t *testing.T) {
	store := newTestStore(t)
	_, err := store.DeleteGroup("master")
	requireErrorCode(t, err, "validation_failed")
	_, err = store.RenameGroup("master", "other")
	requireErrorCode(t, err, "validation_failed")
	_, err = store.CreateGroup("Bad Name")
	requireErrorCode(t, err, "validation_failed")

	if _, err := store.CreateGroup("work"); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateGroup("work")
	requireErrorCode(t, err, "conflict")

	task, err := store.CreateTask(TaskFields{Title: stringPointer("x"), Group: stringPointer("work")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DeleteGroup("work")
	requireErrorCode(t, err, "conflict")

	if _, err := store.RenameGroup("work", "job"); err != nil {
		t.Fatal(err)
	}
	task, err = store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Group != "job" {
		t.Fatalf("rename did not cascade, task group is %q", task.Group)
	}
}

func TestTaskIDsAreNotReused(t *testing.T) {
	store := newTestStore(t)
	first, _ := store.CreateTask(TaskFields{Title: stringPointer("a")})
	if _, err := store.DeleteTask(first.ID); err != nil {
		t.Fatal(err)
	}
	second, _ := store.CreateTask(TaskFields{Title: stringPointer("b")})
	if second.ID == first.ID {
		t.Fatalf("id %d was reused", first.ID)
	}
}

func TestHabitCompletions(t *testing.T) {
	store := newTestStore(t)
	habit, err := store.CreateHabit(HabitFields{Title: stringPointer("Gym")})
	if err != nil {
		t.Fatal(err)
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format(dateLayout)
	tomorrow := time.Now().AddDate(0, 0, 1).Format(dateLayout)

	if _, err := store.SetHabitDone(habit.ID, yesterday, true); err != nil {
		t.Fatal(err)
	}
	habit, err = store.SetHabitDone(habit.ID, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !habit.DoneToday || habit.CurrentStreak != 2 || habit.TotalDaysDone != 2 {
		t.Fatalf("unexpected stats: %+v", habit)
	}
	_, err = store.SetHabitDone(habit.ID, tomorrow, true)
	requireErrorCode(t, err, "validation_failed")

	habit, err = store.SetHabitDone(habit.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if habit.DoneToday || habit.CurrentStreak != 1 {
		t.Fatalf("unexpected stats after undo: %+v", habit)
	}
}
