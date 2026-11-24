package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

type apiServer struct {
	store    *Store
	apiToken string
}

func (server *apiServer) routes() http.Handler {
	store := server.store
	mux := http.NewServeMux()
	route := func(pattern string, handler func(r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			value, err := handler(r)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, value)
		})
	}

	route("GET /api/tasks", func(r *http.Request) (any, error) {
		return store.ListTasks(r.URL.Query().Get("group"), r.URL.Query().Get("all") == "true")
	})
	route("POST /api/tasks", func(r *http.Request) (any, error) {
		var fields TaskFields
		if err := decodeBody(r, &fields); err != nil {
			return nil, err
		}
		return store.CreateTask(fields)
	})
	route("GET /api/tasks/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return store.GetTask(id)
	})
	route("PATCH /api/tasks/{id}", func(r *http.Request) (any, error) {
		var fields TaskFields
		id, err := pathID(r)
		if err == nil {
			err = decodeBody(r, &fields)
		}
		if err != nil {
			return nil, err
		}
		return store.UpdateTask(id, fields)
	})
	route("DELETE /api/tasks/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return store.DeleteTask(id)
	})
	route("POST /api/tasks/{id}/done", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return store.SetTaskDone(id, true)
	})
	route("POST /api/tasks/{id}/reopen", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return store.SetTaskDone(id, false)
	})

	route("GET /api/groups", func(r *http.Request) (any, error) {
		return store.ListGroups()
	})
	route("POST /api/groups", func(r *http.Request) (any, error) {
		var body struct{ Name string }
		if err := decodeBody(r, &body); err != nil {
			return nil, err
		}
		return store.CreateGroup(body.Name)
	})
	route("PATCH /api/groups/{name}", func(r *http.Request) (any, error) {
		var body struct{ Name string }
		if err := decodeBody(r, &body); err != nil {
			return nil, err
		}
		return store.RenameGroup(r.PathValue("name"), body.Name)
	})
	route("DELETE /api/groups/{name}", func(r *http.Request) (any, error) {
		return store.DeleteGroup(r.PathValue("name"))
	})

	route("GET /api/habits", func(r *http.Request) (any, error) {
		return store.ListHabits()
	})
	route("POST /api/habits", func(r *http.Request) (any, error) {
		var fields HabitFields
		if err := decodeBody(r, &fields); err != nil {
			return nil, err
		}
		return store.CreateHabit(fields)
	})
	route("GET /api/habits/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		sinceDate := r.URL.Query().Get("since")
		if sinceDate == "" {
			sinceDate = time.Now().AddDate(0, 0, -29).Format(dateLayout)
		} else if err := validateDate(sinceDate); err != nil {
			return nil, err
		}
		return store.GetHabit(id, sinceDate)
	})
	route("PATCH /api/habits/{id}", func(r *http.Request) (any, error) {
		var fields HabitFields
		id, err := pathID(r)
		if err == nil {
			err = decodeBody(r, &fields)
		}
		if err != nil {
			return nil, err
		}
		return store.UpdateHabit(id, fields)
	})
	route("DELETE /api/habits/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return store.DeleteHabit(id)
	})
	route("POST /api/habits/{id}/completions", func(r *http.Request) (any, error) {
		var body struct{ Date string }
		id, err := pathID(r)
		if err == nil {
			err = decodeBody(r, &body)
		}
		if err != nil {
			return nil, err
		}
		return store.SetHabitDone(id, body.Date, true)
	})
	route("DELETE /api/habits/{id}/completions/{date}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return store.SetHabitDone(id, r.PathValue("date"), false)
	})

	route("/", func(r *http.Request) (any, error) {
		return nil, errNotFound("no route for %s %s", r.Method, r.URL.Path)
	})
	return server.requireToken(mux)
}

func (server *apiServer) requireToken(next http.Handler) http.Handler {
	expected := []byte("Bearer " + server.apiToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
			writeError(w, &apiError{"unauthorized", "missing or wrong API token: check SHTTODO_API_TOKEN", http.StatusUnauthorized})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errValidationFailed("invalid id %q: must be a number", r.PathValue("id"))
	}
	return id, nil
}

// decodeBody accepts an empty body as an empty object.
func decodeBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return errValidationFailed("invalid JSON body: %v", err)
	}
	return nil
}

func writeError(w http.ResponseWriter, err error) {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		log.Printf("internal error: %v", err)
		apiErr = &apiError{"internal_error", "internal server error, see server logs", http.StatusInternalServerError}
	}
	writeJSON(w, apiErr.HTTPStatus, map[string]*apiError{"error": apiErr})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
