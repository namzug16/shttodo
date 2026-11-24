package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

const (
	reminderItemLimit       = 50
	reminderTextLimitBytes  = 3800 // Telegram allows 4096 characters per message.
	lastReminderMessageKey  = "last_reminder_message_id"
	reminderTitleLimitRunes = 100
	buttonTextLimitRunes    = 40
)

type reminder struct {
	store           *Store
	bot             *telegramBot
	intervalMinutes int
	activeStartHour int
	activeEndHour   int
}

// parseActiveHours reads "HH-HH". Both hours are inclusive; start > end wraps past midnight.
func parseActiveHours(value string) (startHour, endHour int, err error) {
	startText, endText, found := strings.Cut(value, "-")
	startHour, startErr := strconv.Atoi(startText)
	endHour, endErr := strconv.Atoi(endText)
	if !found || startErr != nil || endErr != nil || startHour < 0 || startHour > 23 || endHour < 0 || endHour > 23 {
		return 0, 0, fmt.Errorf("invalid REMINDER_ACTIVE_HOURS %q: use HH-HH, for example 09-21", value)
	}
	return startHour, endHour, nil
}

func withinActiveHours(moment time.Time, startHour, endHour int) bool {
	hour := moment.Hour()
	if startHour <= endHour {
		return hour >= startHour && hour <= endHour
	}
	return hour >= startHour || hour <= endHour
}

// nextReminderTime aligns to the wall clock from midnight. intervalMinutes must divide 1440.
func nextReminderTime(now time.Time, intervalMinutes int) time.Time {
	minutesSinceMidnight := now.Hour()*60 + now.Minute()
	nextMinute := (minutesSinceMidnight/intervalMinutes + 1) * intervalMinutes
	return time.Date(now.Year(), now.Month(), now.Day(), 0, nextMinute, 0, 0, now.Location())
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

// buildReminder renders pending habits, then pending tasks in list order. An empty keyboard means nothing is pending.
func buildReminder(habits []Habit, tasks []Task, today string) (string, [][]inlineButton) {
	type reminderItem struct{ section, line, buttonText, callbackData string }
	var items []reminderItem
	for _, habit := range habits {
		if !habit.DoneToday {
			title := truncateRunes(habit.Title, reminderTitleLimitRunes)
			items = append(items, reminderItem{"Today", "⬜ " + title, "✅ " + habit.Title, fmt.Sprintf("habit:%d", habit.ID)})
		}
	}
	for _, task := range tasks {
		marker := ""
		if task.Due != "" && task.Due < today {
			marker = "⚠️ "
		} else if task.Due == today {
			marker = "📅 "
		}
		line := fmt.Sprintf("#%d %s%s", task.ID, marker, truncateRunes(task.Title, reminderTitleLimitRunes))
		if task.Priority == "high" {
			line += " (high)"
		}
		if task.Due != "" {
			line += " due " + task.Due
		}
		items = append(items, reminderItem{task.Group, line, fmt.Sprintf("✅ #%d %s", task.ID, task.Title), fmt.Sprintf("task:%d", task.ID)})
	}

	var text strings.Builder
	var keyboard [][]inlineButton
	currentSection := ""
	for _, item := range items {
		addition := ""
		if item.section != currentSection {
			addition = "\n" + item.section + "\n"
		}
		addition += "  " + item.line + "\n"
		if len(keyboard) == reminderItemLimit || text.Len()+len(addition) > reminderTextLimitBytes {
			break
		}
		text.WriteString(addition)
		currentSection = item.section
		keyboard = append(keyboard, []inlineButton{{truncateRunes(item.buttonText, buttonTextLimitRunes), item.callbackData}})
	}
	if hidden := len(items) - len(keyboard); hidden > 0 {
		fmt.Fprintf(&text, "\n+%d more", hidden)
	}
	return strings.TrimSpace(text.String()), keyboard
}

func (r *reminder) render() (string, [][]inlineButton, error) {
	habits, err := r.store.ListHabits()
	if err != nil {
		return "", nil, err
	}
	tasks, err := r.store.ListTasks("", false)
	if err != nil {
		return "", nil, err
	}
	text, keyboard := buildReminder(habits, tasks, todayDate())
	return text, keyboard, nil
}

func (r *reminder) runSchedule(ctx context.Context) {
	for {
		next := nextReminderTime(time.Now(), r.intervalMinutes)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		if withinActiveHours(next, r.activeStartHour, r.activeEndHour) {
			if err := r.send(ctx); err != nil {
				log.Printf("reminder: %v", err)
			}
		}
	}
}

// send replaces the previous reminder, so only one message has live buttons.
func (r *reminder) send(ctx context.Context) error {
	text, keyboard, err := r.render()
	if err != nil {
		return err
	}
	previousMessageID, err := r.store.GetState(lastReminderMessageKey)
	if err != nil {
		return err
	}
	if previousMessageID != "" {
		messageID, _ := strconv.ParseInt(previousMessageID, 10, 64)
		// Telegram refuses to delete messages older than 48 hours; that is not a reason to skip this reminder.
		if err := r.bot.call(ctx, "deleteMessage", map[string]any{"chat_id": r.bot.chatID, "message_id": messageID}, nil); err != nil {
			log.Printf("reminder: %v", err)
		}
		if err := r.store.SetState(lastReminderMessageKey, ""); err != nil {
			return err
		}
	}
	if len(keyboard) == 0 {
		return nil
	}
	var message telegramMessage
	if err := r.bot.call(ctx, "sendMessage", map[string]any{
		"chat_id": r.bot.chatID, "text": text, "reply_markup": inlineKeyboard{keyboard},
	}, &message); err != nil {
		return err
	}
	return r.store.SetState(lastReminderMessageKey, strconv.FormatInt(message.MessageID, 10))
}

func (r *reminder) pollButtonPresses(ctx context.Context) {
	var nextUpdateOffset int64
	for ctx.Err() == nil {
		var updates []telegramUpdate
		err := r.bot.call(ctx, "getUpdates", map[string]any{
			"offset": nextUpdateOffset, "timeout": 50, "allowed_updates": []string{"callback_query"},
		}, &updates)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("reminder: %v", err)
				time.Sleep(5 * time.Second)
			}
			continue
		}
		for _, update := range updates {
			nextUpdateOffset = update.UpdateID + 1
			if update.CallbackQuery != nil {
				r.handleButtonPress(ctx, update.CallbackQuery)
			}
		}
	}
}

func (r *reminder) handleButtonPress(ctx context.Context, query *telegramCallbackQuery) {
	if query.Message == nil || query.Message.Chat.ID != r.bot.chatID {
		return
	}
	kind, idText, _ := strings.Cut(query.Data, ":")
	id, _ := strconv.ParseInt(idText, 10, 64)
	var answer string
	var err error
	switch kind {
	case "task":
		var task Task
		task, err = r.store.SetTaskDone(id, true)
		answer = "Done: " + task.Title
	case "habit":
		var habit Habit
		habit, err = r.store.SetHabitDone(id, "", true)
		answer = fmt.Sprintf("Done: %s (streak %d)", habit.Title, habit.CurrentStreak)
	default:
		err = fmt.Errorf("unknown button %q", query.Data)
	}
	if err != nil {
		answer = "Error: " + err.Error()
	}
	if err := r.bot.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": query.ID, "text": answer}, nil); err != nil {
		log.Printf("reminder: %v", err)
	}

	text, keyboard, err := r.render()
	if err != nil {
		log.Printf("reminder: %v", err)
		return
	}
	edit := map[string]any{"chat_id": r.bot.chatID, "message_id": query.Message.MessageID, "text": "All done ✅"}
	if len(keyboard) > 0 {
		edit["text"] = text
		edit["reply_markup"] = inlineKeyboard{keyboard}
	}
	if err := r.bot.call(ctx, "editMessageText", edit, nil); err != nil {
		log.Printf("reminder: %v", err)
	}
}
