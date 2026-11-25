package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

const telegramHelpText = `Send a message to add a task. Markers go at the end of the first line:
  #group  !high or !low  @today @tomorrow @mon..@sun @YYYY-MM-DD
More lines become notes.

/add Review PRs #work !high @tomorrow
/habit Gym
/list  send the current reminder now
/help  this text`

var telegramCommandMenu = []map[string]string{
	{"command": "add", "description": "Add a task: title #group !high @tomorrow"},
	{"command": "habit", "description": "Add a daily habit"},
	{"command": "list", "description": "Send the current reminder now"},
	{"command": "help", "description": "Show the syntax"},
}

var weekdaysByMarker = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

func (r *reminder) reply(ctx context.Context, text string) {
	if text == "" {
		return
	}
	if err := r.bot.call(ctx, "sendMessage", map[string]any{"chat_id": r.bot.chatID, "text": text}, nil); err != nil {
		log.Printf("reminder: %v", err)
	}
}

// handleTextMessage returns the reply. Plain text without a command adds a task.
func (r *reminder) handleTextMessage(ctx context.Context, text string) string {
	text = strings.TrimSpace(text)
	command, arguments := "/add", text
	if strings.HasPrefix(text, "/") {
		if index := strings.IndexAny(text, " \n"); index >= 0 {
			command, arguments = text[:index], strings.TrimSpace(text[index:])
		} else {
			command, arguments = text, ""
		}
		// Telegram adds @botname to commands in group chats.
		command, _, _ = strings.Cut(command, "@")
	}

	switch command {
	case "/add":
		fields, err := parseTaskMessage(arguments, time.Now())
		if err != nil {
			return "Error: " + err.Error()
		}
		task, err := r.store.CreateTask(fields)
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.Code == "not_found" && fields.Group != nil {
			return fmt.Sprintf("Error: group %q does not exist. Groups: %s", *fields.Group, r.groupNames())
		}
		if err != nil {
			return "Error: " + err.Error()
		}
		details := []string{task.Group}
		if task.Priority != "normal" {
			details = append(details, task.Priority)
		}
		if task.Due != "" {
			details = append(details, "due "+task.Due)
		}
		return fmt.Sprintf("Added #%d %s (%s)", task.ID, task.Title, strings.Join(details, ", "))
	case "/habit":
		title, notes, _ := strings.Cut(arguments, "\n")
		notes = strings.TrimSpace(notes)
		habit, err := r.store.CreateHabit(HabitFields{Title: &title, Notes: &notes})
		if err != nil {
			return "Error: " + err.Error()
		}
		return fmt.Sprintf("Added habit #%d %s", habit.ID, habit.Title)
	case "/list":
		sent, err := r.send(ctx)
		if err != nil {
			return "Error: " + err.Error()
		}
		if !sent {
			return "Nothing pending ✅"
		}
		return ""
	case "/help", "/start":
		return telegramHelpText
	}
	return "Unknown command " + command + ". Send /help."
}

func (r *reminder) groupNames() string {
	groups, err := r.store.ListGroups()
	if err != nil {
		return "unknown"
	}
	names := make([]string, len(groups))
	for index, group := range groups {
		names[index] = group.Name
	}
	return strings.Join(names, ", ")
}

// parseTaskMessage reads "title [#group] [!high|!low] [@date]" on the first line; more lines are notes.
// Markers count only at the end, so titles can contain #, ! and @.
func parseTaskMessage(text string, now time.Time) (TaskFields, error) {
	firstLine, notes, _ := strings.Cut(strings.TrimSpace(text), "\n")
	words := strings.Fields(firstLine)
	var fields TaskFields
	for len(words) > 1 {
		word := words[len(words)-1]
		if len(word) < 2 || !strings.ContainsRune("#!@", rune(word[0])) {
			break
		}
		value := word[1:]
		var target **string
		switch word[0] {
		case '#':
			target = &fields.Group
		case '!':
			if value != "high" && value != "low" {
				return fields, fmt.Errorf("unknown priority marker %s: use !high or !low", word)
			}
			target = &fields.Priority
		case '@':
			due, err := resolveDueMarker(value, now)
			if err != nil {
				return fields, err
			}
			value = due
			target = &fields.Due
		}
		if *target != nil {
			return fields, fmt.Errorf("marker %s is given twice", word[:1])
		}
		*target = &value
		words = words[:len(words)-1]
	}
	if len(words) == 0 {
		return fields, errors.New("the title is empty")
	}
	title := strings.Join(words, " ")
	fields.Title = &title
	if notes = strings.TrimSpace(notes); notes != "" {
		fields.Notes = &notes
	}
	return fields, nil
}

// resolveDueMarker accepts today, tomorrow, a weekday (the next one, never today) or YYYY-MM-DD.
func resolveDueMarker(value string, now time.Time) (string, error) {
	switch value = strings.ToLower(value); value {
	case "today":
		return now.Format(dateLayout), nil
	case "tomorrow":
		return now.AddDate(0, 0, 1).Format(dateLayout), nil
	}
	if weekday, found := weekdaysByMarker[value]; found {
		daysAhead := (int(weekday) - int(now.Weekday()) + 7) % 7
		if daysAhead == 0 {
			daysAhead = 7
		}
		return now.AddDate(0, 0, daysAhead).Format(dateLayout), nil
	}
	if validateDate(value) != nil {
		return "", fmt.Errorf("unknown date @%s: use @today, @tomorrow, @mon..@sun or @YYYY-MM-DD", value)
	}
	return value, nil
}
