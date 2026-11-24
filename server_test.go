package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAPIToken = "test-token"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer((&apiServer{store: newTestStore(t), apiToken: testAPIToken}).routes())
	t.Cleanup(server.Close)
	return server
}

func callAPI(t *testing.T, server *httptest.Server, token, method, path, body string, wantStatus int, target any) {
	t.Helper()
	request, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: got status %d, want %d", method, path, response.StatusCode, wantStatus)
	}
	if target != nil {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAPIRejectsWrongToken(t *testing.T) {
	server := newTestServer(t)
	var body struct{ Error apiError }
	callAPI(t, server, "wrong", "GET", "/api/tasks", "", http.StatusUnauthorized, &body)
	if body.Error.Code != "unauthorized" {
		t.Fatalf("got %+v", body)
	}
}

func TestAPITaskLifecycle(t *testing.T) {
	server := newTestServer(t)
	var task Task
	callAPI(t, server, testAPIToken, "POST", "/api/tasks", `{"title":"Pay invoice","priority":"high","due":"2026-12-01"}`, http.StatusOK, &task)
	if task.ID == 0 || task.Group != "master" || task.Status != "pending" {
		t.Fatalf("unexpected task: %+v", task)
	}

	callAPI(t, server, testAPIToken, "PATCH", "/api/tasks/1", `{"due":"","notes":"ask finance"}`, http.StatusOK, &task)
	if task.Due != "" || task.Notes != "ask finance" || task.Title != "Pay invoice" {
		t.Fatalf("patch changed wrong fields: %+v", task)
	}

	callAPI(t, server, testAPIToken, "POST", "/api/tasks/1/done", "", http.StatusOK, &task)
	if task.Status != "done" || task.CompletedAt == nil {
		t.Fatalf("not done: %+v", task)
	}
	var tasks []Task
	callAPI(t, server, testAPIToken, "GET", "/api/tasks", "", http.StatusOK, &tasks)
	if len(tasks) != 0 {
		t.Fatalf("done task is listed: %+v", tasks)
	}
	callAPI(t, server, testAPIToken, "GET", "/api/tasks?all=true", "", http.StatusOK, &tasks)
	if len(tasks) != 1 {
		t.Fatalf("--all misses done task: %+v", tasks)
	}

	callAPI(t, server, testAPIToken, "POST", "/api/tasks/1/reopen", "", http.StatusOK, &task)
	if task.Status != "pending" || task.CompletedAt != nil {
		t.Fatalf("not reopened: %+v", task)
	}
	callAPI(t, server, testAPIToken, "DELETE", "/api/tasks/1", "", http.StatusOK, nil)
	callAPI(t, server, testAPIToken, "GET", "/api/tasks/1", "", http.StatusNotFound, nil)
}

func TestAPIValidationErrors(t *testing.T) {
	server := newTestServer(t)
	var body struct{ Error apiError }
	callAPI(t, server, testAPIToken, "POST", "/api/tasks", `{"title":"x","priority":"urgent"}`, http.StatusBadRequest, &body)
	if body.Error.Code != "validation_failed" {
		t.Fatalf("got %+v", body)
	}
	callAPI(t, server, testAPIToken, "POST", "/api/tasks", `{"title":"x","group":"nope"}`, http.StatusNotFound, nil)
	callAPI(t, server, testAPIToken, "POST", "/api/tasks", `{"title":"x","unknown":1}`, http.StatusBadRequest, nil)
	callAPI(t, server, testAPIToken, "GET", "/api/tasks/abc", "", http.StatusBadRequest, nil)
	callAPI(t, server, testAPIToken, "GET", "/api/nothing", "", http.StatusNotFound, nil)
}

func TestAPIGroupsAndHabits(t *testing.T) {
	server := newTestServer(t)
	var group Group
	callAPI(t, server, testAPIToken, "POST", "/api/groups", `{"name":"work"}`, http.StatusOK, &group)
	callAPI(t, server, testAPIToken, "PATCH", "/api/groups/work", `{"name":"job"}`, http.StatusOK, &group)
	if group.Name != "job" {
		t.Fatalf("rename failed: %+v", group)
	}
	callAPI(t, server, testAPIToken, "DELETE", "/api/groups/job", "", http.StatusOK, nil)

	var habit Habit
	callAPI(t, server, testAPIToken, "POST", "/api/habits", `{"title":"Gym"}`, http.StatusOK, &habit)
	callAPI(t, server, testAPIToken, "POST", "/api/habits/1/completions", "", http.StatusOK, &habit)
	if !habit.DoneToday || habit.CurrentStreak != 1 {
		t.Fatalf("completion failed: %+v", habit)
	}
	callAPI(t, server, testAPIToken, "GET", "/api/habits/1", "", http.StatusOK, &habit)
	if len(habit.CompletionDates) != 1 {
		t.Fatalf("show misses completion dates: %+v", habit)
	}
	callAPI(t, server, testAPIToken, "DELETE", "/api/habits/1/completions/"+todayDate(), "", http.StatusOK, &habit)
	if habit.DoneToday {
		t.Fatalf("undo failed: %+v", habit)
	}
}
