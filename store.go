package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultGroupName = "master"
	dateLayout       = "2006-01-02"
)

var (
	groupNamePattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	validPriorities  = map[string]bool{"low": true, "normal": true, "high": true}
)

// apiError is the single error type shared by the store, the REST API and the CLI.
type apiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *apiError) Error() string { return e.Message }

func errNotFound(format string, args ...any) *apiError {
	return &apiError{"not_found", fmt.Sprintf(format, args...), http.StatusNotFound}
}

func errValidationFailed(format string, args ...any) *apiError {
	return &apiError{"validation_failed", fmt.Sprintf(format, args...), http.StatusBadRequest}
}

func errConflict(format string, args ...any) *apiError {
	return &apiError{"conflict", fmt.Sprintf(format, args...), http.StatusConflict}
}

type Task struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Group       string  `json:"group"`
	Priority    string  `json:"priority"`
	Due         string  `json:"due"`
	Notes       string  `json:"notes"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`
}

type Group struct {
	Name             string `json:"name"`
	PendingTaskCount int    `json:"pending_task_count"`
	TotalTaskCount   int    `json:"total_task_count"`
}

type Habit struct {
	ID              int64    `json:"id"`
	Title           string   `json:"title"`
	Notes           string   `json:"notes"`
	CreatedAt       string   `json:"created_at"`
	DoneToday       bool     `json:"done_today"`
	CurrentStreak   int      `json:"current_streak"`
	LongestStreak   int      `json:"longest_streak"`
	TotalDaysDone   int      `json:"total_days_done"`
	CompletionDates []string `json:"completion_dates,omitempty"`
}

// TaskFields holds task input. Nil means "not provided".
type TaskFields struct {
	Title    *string `json:"title"`
	Group    *string `json:"group"`
	Priority *string `json:"priority"`
	Due      *string `json:"due"`
	Notes    *string `json:"notes"`
}

type HabitFields struct {
	Title *string `json:"title"`
	Notes *string `json:"notes"`
}

type Store struct {
	db *sql.DB
}

var migrations = []string{
	`CREATE TABLE groups (name TEXT PRIMARY KEY);
	INSERT INTO groups (name) VALUES ('master');
	CREATE TABLE tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		group_name TEXT NOT NULL REFERENCES groups(name) ON UPDATE CASCADE,
		priority TEXT NOT NULL DEFAULT 'normal',
		due TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TEXT NOT NULL,
		completed_at TEXT
	);
	CREATE TABLE habits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		notes TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE TABLE habit_completions (
		habit_id INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
		date TEXT NOT NULL,
		PRIMARY KEY (habit_id, date)
	);
	CREATE TABLE state (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
}

func openStore(databasePath string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite writes serialized and the pragmas applied.
	db.SetMaxOpenConns(1)
	var schemaVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		return nil, fmt.Errorf("open database %s: %w", databasePath, err)
	}
	for version := schemaVersion; version < len(migrations); version++ {
		tx, err := db.Begin()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(migrations[version]); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("migration %d: %w", version+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version+1)); err != nil {
			tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

func nowTimestamp() string { return time.Now().UTC().Format(time.RFC3339) }

func todayDate() string { return time.Now().Format(dateLayout) }

func validateDate(value string) *apiError {
	if _, err := time.Parse(dateLayout, value); err != nil {
		return errValidationFailed("invalid date %q: use YYYY-MM-DD", value)
	}
	return nil
}

func (s *Store) groupExists(name string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM groups WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

func (s *Store) validateTaskFields(fields TaskFields) error {
	if fields.Title != nil && strings.TrimSpace(*fields.Title) == "" {
		return errValidationFailed("title must not be empty")
	}
	if fields.Priority != nil && !validPriorities[*fields.Priority] {
		return errValidationFailed("invalid priority %q: use low, normal or high", *fields.Priority)
	}
	if fields.Due != nil && *fields.Due != "" {
		if err := validateDate(*fields.Due); err != nil {
			return err
		}
	}
	if fields.Group != nil {
		exists, err := s.groupExists(*fields.Group)
		if err != nil {
			return err
		}
		if !exists {
			return errNotFound("group %q does not exist: create it with `shttodo group create %s`", *fields.Group, *fields.Group)
		}
	}
	return nil
}

const taskColumns = `id, title, group_name, priority, due, notes, status, created_at, completed_at`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var task Task
	err := row.Scan(&task.ID, &task.Title, &task.Group, &task.Priority, &task.Due, &task.Notes, &task.Status, &task.CreatedAt, &task.CompletedAt)
	return task, err
}

func (s *Store) GetTask(id int64) (Task, error) {
	task, err := scanTask(s.db.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return task, errNotFound("task %d does not exist", id)
	}
	return task, err
}

func (s *Store) ListTasks(groupName string, includeDone bool) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE (? = '' OR group_name = ?)`
	if !includeDone {
		query += ` AND status = 'pending'`
	}
	query += ` ORDER BY group_name, CASE priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END, due = '', due, id`
	if groupName != "" {
		exists, err := s.groupExists(groupName)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errNotFound("group %q does not exist", groupName)
		}
	}
	rows, err := s.db.Query(query, groupName, groupName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) CreateTask(fields TaskFields) (Task, error) {
	if fields.Title == nil {
		return Task{}, errValidationFailed("title is required")
	}
	if err := s.validateTaskFields(fields); err != nil {
		return Task{}, err
	}
	valueOr := func(value *string, fallback string) string {
		if value == nil {
			return fallback
		}
		return *value
	}
	result, err := s.db.Exec(`INSERT INTO tasks (title, group_name, priority, due, notes, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(*fields.Title), valueOr(fields.Group, defaultGroupName), valueOr(fields.Priority, "normal"),
		valueOr(fields.Due, ""), valueOr(fields.Notes, ""), nowTimestamp())
	if err != nil {
		return Task{}, err
	}
	id, _ := result.LastInsertId()
	return s.GetTask(id)
}

func (s *Store) UpdateTask(id int64, fields TaskFields) (Task, error) {
	if _, err := s.GetTask(id); err != nil {
		return Task{}, err
	}
	if err := s.validateTaskFields(fields); err != nil {
		return Task{}, err
	}
	columns := []struct {
		name  string
		value *string
	}{{"title", fields.Title}, {"group_name", fields.Group}, {"priority", fields.Priority}, {"due", fields.Due}, {"notes", fields.Notes}}
	for _, column := range columns {
		if column.value == nil {
			continue
		}
		value := *column.value
		if column.name == "title" {
			value = strings.TrimSpace(value)
		}
		if _, err := s.db.Exec(`UPDATE tasks SET `+column.name+` = ? WHERE id = ?`, value, id); err != nil {
			return Task{}, err
		}
	}
	return s.GetTask(id)
}

func (s *Store) SetTaskDone(id int64, done bool) (Task, error) {
	task, err := s.GetTask(id)
	if err != nil {
		return Task{}, err
	}
	if done && task.Status == "pending" {
		_, err = s.db.Exec(`UPDATE tasks SET status = 'done', completed_at = ? WHERE id = ?`, nowTimestamp(), id)
	} else if !done && task.Status == "done" {
		_, err = s.db.Exec(`UPDATE tasks SET status = 'pending', completed_at = NULL WHERE id = ?`, id)
	}
	if err != nil {
		return Task{}, err
	}
	return s.GetTask(id)
}

func (s *Store) DeleteTask(id int64) (Task, error) {
	task, err := s.GetTask(id)
	if err != nil {
		return Task{}, err
	}
	_, err = s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return task, err
}

func (s *Store) GetGroup(name string) (Group, error) {
	group := Group{Name: name}
	exists, err := s.groupExists(name)
	if err != nil {
		return group, err
	}
	if !exists {
		return group, errNotFound("group %q does not exist", name)
	}
	err = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'pending'), 0) FROM tasks WHERE group_name = ?`, name).
		Scan(&group.TotalTaskCount, &group.PendingTaskCount)
	return group, err
}

func (s *Store) ListGroups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT g.name, COUNT(t.id), COALESCE(SUM(t.status = 'pending'), 0)
		FROM groups g LEFT JOIN tasks t ON t.group_name = g.name GROUP BY g.name ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []Group{}
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.Name, &group.TotalTaskCount, &group.PendingTaskCount); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func validateGroupName(name string) *apiError {
	if !groupNamePattern.MatchString(name) {
		return errValidationFailed("invalid group name %q: use 1-32 characters of a-z, 0-9 and -", name)
	}
	return nil
}

func (s *Store) CreateGroup(name string) (Group, error) {
	if err := validateGroupName(name); err != nil {
		return Group{}, err
	}
	exists, err := s.groupExists(name)
	if err != nil {
		return Group{}, err
	}
	if exists {
		return Group{}, errConflict("group %q already exists", name)
	}
	if _, err := s.db.Exec(`INSERT INTO groups (name) VALUES (?)`, name); err != nil {
		return Group{}, err
	}
	return s.GetGroup(name)
}

func (s *Store) RenameGroup(oldName, newName string) (Group, error) {
	if oldName == defaultGroupName {
		return Group{}, errValidationFailed("group %q is permanent and cannot be renamed", defaultGroupName)
	}
	if _, err := s.GetGroup(oldName); err != nil {
		return Group{}, err
	}
	if err := validateGroupName(newName); err != nil {
		return Group{}, err
	}
	exists, err := s.groupExists(newName)
	if err != nil {
		return Group{}, err
	}
	if exists {
		return Group{}, errConflict("group %q already exists", newName)
	}
	if _, err := s.db.Exec(`UPDATE groups SET name = ? WHERE name = ?`, newName, oldName); err != nil {
		return Group{}, err
	}
	return s.GetGroup(newName)
}

func (s *Store) DeleteGroup(name string) (Group, error) {
	if name == defaultGroupName {
		return Group{}, errValidationFailed("group %q is permanent and cannot be deleted", defaultGroupName)
	}
	group, err := s.GetGroup(name)
	if err != nil {
		return Group{}, err
	}
	if group.TotalTaskCount > 0 {
		return Group{}, errConflict("group %q still has %d tasks (%d pending): move them with `shttodo edit <id> --group <other>` or delete them first",
			name, group.TotalTaskCount, group.PendingTaskCount)
	}
	_, err = s.db.Exec(`DELETE FROM groups WHERE name = ?`, name)
	return group, err
}

func (s *Store) habitCompletionDates(habitID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT date FROM habit_completions WHERE habit_id = ? ORDER BY date`, habitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dates := []string{}
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}
	return dates, rows.Err()
}

// fillHabitStats sets the calculated fields. completion dates are kept only from sinceDate on; "" drops them.
func (s *Store) fillHabitStats(habit *Habit, sinceDate string) error {
	dates, err := s.habitCompletionDates(habit.ID)
	if err != nil {
		return err
	}
	today := todayDate()
	habit.TotalDaysDone = len(dates)
	habit.DoneToday = len(dates) > 0 && dates[len(dates)-1] == today
	habit.CurrentStreak, habit.LongestStreak = calculateStreaks(dates, today)
	if sinceDate != "" {
		habit.CompletionDates = []string{}
		for _, date := range dates {
			if date >= sinceDate {
				habit.CompletionDates = append(habit.CompletionDates, date)
			}
		}
	}
	return nil
}

// calculateStreaks expects sorted, unique YYYY-MM-DD dates. The current streak stays alive until today ends.
func calculateStreaks(sortedDates []string, today string) (currentStreak, longestStreak int) {
	runLength := 0
	var previousDay time.Time
	for _, date := range sortedDates {
		day, _ := time.Parse(dateLayout, date)
		if runLength > 0 && previousDay.AddDate(0, 0, 1).Equal(day) {
			runLength++
		} else {
			runLength = 1
		}
		longestStreak = max(longestStreak, runLength)
		previousDay = day
	}
	if runLength == 0 {
		return 0, 0
	}
	todayDay, _ := time.Parse(dateLayout, today)
	if previousDay.Equal(todayDay) || previousDay.AddDate(0, 0, 1).Equal(todayDay) {
		currentStreak = runLength
	}
	return currentStreak, longestStreak
}

func (s *Store) GetHabit(id int64, sinceDate string) (Habit, error) {
	var habit Habit
	err := s.db.QueryRow(`SELECT id, title, notes, created_at FROM habits WHERE id = ?`, id).
		Scan(&habit.ID, &habit.Title, &habit.Notes, &habit.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return habit, errNotFound("habit %d does not exist", id)
	}
	if err != nil {
		return habit, err
	}
	return habit, s.fillHabitStats(&habit, sinceDate)
}

func (s *Store) ListHabits() ([]Habit, error) {
	rows, err := s.db.Query(`SELECT id FROM habits ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var habitIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		habitIDs = append(habitIDs, id)
	}
	rows.Close()
	habits := []Habit{}
	for _, id := range habitIDs {
		habit, err := s.GetHabit(id, "")
		if err != nil {
			return nil, err
		}
		habits = append(habits, habit)
	}
	return habits, nil
}

func (s *Store) CreateHabit(fields HabitFields) (Habit, error) {
	if fields.Title == nil || strings.TrimSpace(*fields.Title) == "" {
		return Habit{}, errValidationFailed("title is required")
	}
	notes := ""
	if fields.Notes != nil {
		notes = *fields.Notes
	}
	result, err := s.db.Exec(`INSERT INTO habits (title, notes, created_at) VALUES (?, ?, ?)`, strings.TrimSpace(*fields.Title), notes, nowTimestamp())
	if err != nil {
		return Habit{}, err
	}
	id, _ := result.LastInsertId()
	return s.GetHabit(id, "")
}

func (s *Store) UpdateHabit(id int64, fields HabitFields) (Habit, error) {
	if _, err := s.GetHabit(id, ""); err != nil {
		return Habit{}, err
	}
	if fields.Title != nil {
		if strings.TrimSpace(*fields.Title) == "" {
			return Habit{}, errValidationFailed("title must not be empty")
		}
		if _, err := s.db.Exec(`UPDATE habits SET title = ? WHERE id = ?`, strings.TrimSpace(*fields.Title), id); err != nil {
			return Habit{}, err
		}
	}
	if fields.Notes != nil {
		if _, err := s.db.Exec(`UPDATE habits SET notes = ? WHERE id = ?`, *fields.Notes, id); err != nil {
			return Habit{}, err
		}
	}
	return s.GetHabit(id, "")
}

func (s *Store) DeleteHabit(id int64) (Habit, error) {
	habit, err := s.GetHabit(id, "")
	if err != nil {
		return Habit{}, err
	}
	_, err = s.db.Exec(`DELETE FROM habits WHERE id = ?`, id)
	return habit, err
}

// SetHabitDone marks or unmarks one day. An empty date means today.
func (s *Store) SetHabitDone(id int64, date string, done bool) (Habit, error) {
	if date == "" {
		date = todayDate()
	}
	if err := validateDate(date); err != nil {
		return Habit{}, err
	}
	if date > todayDate() {
		return Habit{}, errValidationFailed("date %s is in the future", date)
	}
	if _, err := s.GetHabit(id, ""); err != nil {
		return Habit{}, err
	}
	var err error
	if done {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO habit_completions (habit_id, date) VALUES (?, ?)`, id, date)
	} else {
		_, err = s.db.Exec(`DELETE FROM habit_completions WHERE habit_id = ? AND date = ?`, id, date)
	}
	if err != nil {
		return Habit{}, err
	}
	return s.GetHabit(id, "")
}

func (s *Store) GetState(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM state WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) SetState(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
