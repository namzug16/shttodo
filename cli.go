package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const helpText = `shttodo - a stupidly simple, agent-first task manager with Telegram reminders.

Commands:
  add <title> [--group G] [--priority low|normal|high] [--due YYYY-MM-DD] [--notes N]
  list [--group G] [--all]
  show <id>
  edit <id> [--title T] [--group G] [--priority P] [--due D] [--notes N]
  done <id> | reopen <id> | delete <id>
  group create <name> | group list | group rename <old> <new> | group delete <name>
  habit add <title> [--notes N] | habit list | habit show <id> [--since YYYY-MM-DD]
  habit done <id> [--date D] | habit undo <id> [--date D]
  habit edit <id> [--title T] [--notes N] | habit delete <id>
  serve        run the backend (API + Telegram reminders)
  skill        print the full agent skill

Every command accepts --json. An empty flag value clears the field (--due "").

Examples:
  shttodo add "Review PRs" --group work --priority high --due 2026-12-01
  shttodo list --json
  shttodo done 12
  shttodo habit done 3

AI agents: run ` + "`shttodo skill`" + ` for full instructions.
`

type apiClient struct {
	serverURL  string
	apiToken   string
	httpClient *http.Client
}

func newAPIClient() (*apiClient, error) {
	serverURL, err := requiredEnv("SHTTODO_SERVER_URL")
	if err != nil {
		return nil, err
	}
	apiToken, err := requiredEnv("SHTTODO_API_TOKEN")
	if err != nil {
		return nil, err
	}
	return &apiClient{strings.TrimRight(serverURL, "/"), apiToken, &http.Client{Timeout: 15 * time.Second}}, nil
}

func (client *apiClient) call(method, path string, requestBody any, result any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, client.serverURL+path, body)
	if err != nil {
		return errValidationFailed("invalid SHTTODO_SERVER_URL %q: %v", client.serverURL, err)
	}
	request.Header.Set("Authorization", "Bearer "+client.apiToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return &apiError{"server_unreachable", fmt.Sprintf("cannot reach %s: %v", client.serverURL, err), 0}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var errorBody struct{ Error *apiError }
		if json.NewDecoder(response.Body).Decode(&errorBody) != nil || errorBody.Error == nil {
			return &apiError{"server_unreachable", fmt.Sprintf("unexpected HTTP %d from %s", response.StatusCode, client.serverURL), 0}
		}
		return errorBody.Error
	}
	return json.NewDecoder(response.Body).Decode(result)
}

func printError(err error, jsonMode bool) {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		apiErr = &apiError{Code: "internal_error", Message: err.Error()}
	}
	if jsonMode {
		json.NewEncoder(os.Stdout).Encode(map[string]*apiError{"error": apiErr})
		return
	}
	fmt.Fprintln(os.Stderr, "error:", apiErr.Message)
}

// parseArguments allows flags before and after positional arguments.
func parseArguments(flags *flag.FlagSet, arguments []string) ([]string, error) {
	flags.SetOutput(io.Discard)
	flags.Bool("json", false, "")
	var positional []string
	for {
		if err := flags.Parse(arguments); err != nil {
			return nil, errValidationFailed("%v: run `shttodo` for usage", err)
		}
		arguments = flags.Args()
		if len(arguments) == 0 {
			return positional, nil
		}
		positional = append(positional, arguments[0])
		arguments = arguments[1:]
	}
}

func visitedFlags(flags *flag.FlagSet) map[string]bool {
	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	return visited
}

func onlyIfSet(visited map[string]bool, name string, value *string) *string {
	if visited[name] {
		return value
	}
	return nil
}

func requireArguments(positional []string, count int, usage string) error {
	if len(positional) != count {
		return errValidationFailed("usage: shttodo %s", usage)
	}
	return nil
}

func parseID(text string) (int64, error) {
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, errValidationFailed("invalid id %q: must be a number", text)
	}
	return id, nil
}

func runCommand(arguments []string) error {
	if len(arguments) == 0 {
		fmt.Print(helpText)
		return nil
	}
	command, rest := arguments[0], arguments[1:]
	switch command {
	case "help", "-h", "--help":
		fmt.Print(helpText)
		return nil
	case "skill":
		fmt.Print(skillMarkdown)
		return nil
	case "serve":
		return runServe()
	}
	if (command == "group" || command == "habit") && len(rest) == 0 {
		return errValidationFailed("usage: shttodo %s <subcommand>: run `shttodo` for usage", command)
	}
	runSubcommand := map[string]func(client *apiClient, flags *flag.FlagSet) error{
		"group": func(client *apiClient, flags *flag.FlagSet) error {
			return runGroupCommand(client, rest[0], flags, rest[1:])
		},
		"habit": func(client *apiClient, flags *flag.FlagSet) error {
			return runHabitCommand(client, rest[0], flags, rest[1:])
		},
	}[command]
	switch command {
	case "add", "list", "show", "edit", "done", "reopen", "delete":
		runSubcommand = func(client *apiClient, flags *flag.FlagSet) error {
			return runTaskCommand(client, command, flags, rest)
		}
	}
	if runSubcommand == nil {
		return errValidationFailed("unknown command %q: run `shttodo` for usage", command)
	}
	client, err := newAPIClient()
	if err != nil {
		return err
	}
	return runSubcommand(client, flag.NewFlagSet(command, flag.ContinueOnError))
}

func runTaskCommand(client *apiClient, command string, flags *flag.FlagSet, arguments []string) error {
	title := flags.String("title", "", "")
	group := flags.String("group", "", "")
	priority := flags.String("priority", "", "")
	due := flags.String("due", "", "")
	notes := flags.String("notes", "", "")
	includeDone := flags.Bool("all", false, "")
	positional, err := parseArguments(flags, arguments)
	if err != nil {
		return err
	}
	visited := visitedFlags(flags)
	jsonMode := visited["json"]
	fields := TaskFields{
		Title: onlyIfSet(visited, "title", title), Group: onlyIfSet(visited, "group", group),
		Priority: onlyIfSet(visited, "priority", priority), Due: onlyIfSet(visited, "due", due), Notes: onlyIfSet(visited, "notes", notes),
	}

	if command == "list" {
		query := url.Values{}
		query.Set("group", *group)
		query.Set("all", strconv.FormatBool(*includeDone))
		var tasks []Task
		if err := client.call("GET", "/api/tasks?"+query.Encode(), nil, &tasks); err != nil {
			return err
		}
		if jsonMode {
			return printJSON(tasks)
		}
		printTaskList(tasks)
		return nil
	}

	var task Task
	if command == "add" {
		if len(positional) == 0 {
			return errValidationFailed("usage: shttodo add <title> [flags]")
		}
		joinedTitle := strings.Join(positional, " ")
		fields.Title = &joinedTitle
		err = client.call("POST", "/api/tasks", fields, &task)
	} else {
		if err := requireArguments(positional, 1, command+" <id>"); err != nil {
			return err
		}
		id, err := parseID(positional[0])
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/api/tasks/%d", id)
		switch command {
		case "show":
			err = client.call("GET", path, nil, &task)
		case "edit":
			err = client.call("PATCH", path, fields, &task)
		case "done":
			err = client.call("POST", path+"/done", nil, &task)
		case "reopen":
			err = client.call("POST", path+"/reopen", nil, &task)
		case "delete":
			err = client.call("DELETE", path, nil, &task)
		}
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if jsonMode {
		return printJSON(task)
	}
	if command == "show" {
		printTaskDetail(task)
		return nil
	}
	pastTense := map[string]string{"add": "added", "edit": "edited", "done": "completed", "reopen": "reopened", "delete": "deleted"}
	fmt.Printf("%s #%d %s (%s)\n", pastTense[command], task.ID, task.Title, task.Group)
	return nil
}

func runGroupCommand(client *apiClient, subcommand string, flags *flag.FlagSet, arguments []string) error {
	positional, err := parseArguments(flags, arguments)
	if err != nil {
		return err
	}
	jsonMode := visitedFlags(flags)["json"]
	if subcommand == "list" {
		var groups []Group
		if err := client.call("GET", "/api/groups", nil, &groups); err != nil {
			return err
		}
		if jsonMode {
			return printJSON(groups)
		}
		for _, group := range groups {
			fmt.Printf("%-20s %d pending, %d total\n", group.Name, group.PendingTaskCount, group.TotalTaskCount)
		}
		return nil
	}

	var group Group
	switch subcommand {
	case "create":
		if err = requireArguments(positional, 1, "group create <name>"); err == nil {
			err = client.call("POST", "/api/groups", map[string]string{"name": positional[0]}, &group)
		}
	case "rename":
		if err = requireArguments(positional, 2, "group rename <old> <new>"); err == nil {
			err = client.call("PATCH", "/api/groups/"+url.PathEscape(positional[0]), map[string]string{"name": positional[1]}, &group)
		}
	case "delete":
		if err = requireArguments(positional, 1, "group delete <name>"); err == nil {
			err = client.call("DELETE", "/api/groups/"+url.PathEscape(positional[0]), nil, &group)
		}
	default:
		return errValidationFailed("unknown command \"group %s\": use create, list, rename or delete", subcommand)
	}
	if err != nil {
		return err
	}
	if jsonMode {
		return printJSON(group)
	}
	pastTense := map[string]string{"create": "created", "rename": "renamed to", "delete": "deleted"}
	fmt.Printf("%s group %s\n", pastTense[subcommand], group.Name)
	return nil
}

func runHabitCommand(client *apiClient, subcommand string, flags *flag.FlagSet, arguments []string) error {
	title := flags.String("title", "", "")
	notes := flags.String("notes", "", "")
	date := flags.String("date", "", "")
	sinceDate := flags.String("since", "", "")
	positional, err := parseArguments(flags, arguments)
	if err != nil {
		return err
	}
	visited := visitedFlags(flags)
	jsonMode := visited["json"]

	if subcommand == "list" {
		var habits []Habit
		if err := client.call("GET", "/api/habits", nil, &habits); err != nil {
			return err
		}
		if jsonMode {
			return printJSON(habits)
		}
		if len(habits) == 0 {
			fmt.Println("no habits")
		}
		for _, habit := range habits {
			printHabitLine(habit)
		}
		return nil
	}

	var habit Habit
	if subcommand == "add" {
		if len(positional) == 0 {
			return errValidationFailed("usage: shttodo habit add <title> [--notes N]")
		}
		joinedTitle := strings.Join(positional, " ")
		err = client.call("POST", "/api/habits", HabitFields{Title: &joinedTitle, Notes: onlyIfSet(visited, "notes", notes)}, &habit)
	} else {
		if err := requireArguments(positional, 1, "habit "+subcommand+" <id>"); err != nil {
			return err
		}
		id, err := parseID(positional[0])
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/api/habits/%d", id)
		switch subcommand {
		case "show":
			err = client.call("GET", path+"?since="+url.QueryEscape(*sinceDate), nil, &habit)
		case "edit":
			err = client.call("PATCH", path, HabitFields{Title: onlyIfSet(visited, "title", title), Notes: onlyIfSet(visited, "notes", notes)}, &habit)
		case "done":
			err = client.call("POST", path+"/completions", map[string]string{"date": *date}, &habit)
		case "undo":
			undoDate := *date
			if undoDate == "" {
				undoDate = time.Now().Format(dateLayout)
			}
			err = client.call("DELETE", path+"/completions/"+url.PathEscape(undoDate), nil, &habit)
		case "delete":
			err = client.call("DELETE", path, nil, &habit)
		default:
			return errValidationFailed("unknown command \"habit %s\": use add, list, show, done, undo, edit or delete", subcommand)
		}
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if jsonMode {
		return printJSON(habit)
	}
	switch subcommand {
	case "show":
		printHabitDetail(habit)
	case "done", "undo":
		day := *date
		if day == "" {
			day = "today"
		}
		fmt.Printf("%s #%d %s for %s, streak %d\n", map[string]string{"done": "marked", "undo": "unmarked"}[subcommand], habit.ID, habit.Title, day, habit.CurrentStreak)
	default:
		pastTense := map[string]string{"add": "added", "edit": "edited", "delete": "deleted"}
		fmt.Printf("%s habit #%d %s\n", pastTense[subcommand], habit.ID, habit.Title)
	}
	return nil
}

func printJSON(value any) error {
	return json.NewEncoder(os.Stdout).Encode(value)
}

func printTaskList(tasks []Task) {
	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return
	}
	currentGroup := ""
	for _, task := range tasks {
		if task.Group != currentGroup {
			fmt.Println(task.Group)
			currentGroup = task.Group
		}
		priority := task.Priority
		if priority == "normal" {
			priority = ""
		}
		line := fmt.Sprintf("  %-4d %-5s %s", task.ID, priority, task.Title)
		if task.Due != "" {
			line += "  due " + task.Due
		}
		if task.Status == "done" {
			line += "  [done]"
		}
		fmt.Println(line)
	}
}

func printTaskDetail(task Task) {
	fmt.Printf("#%d %s\n", task.ID, task.Title)
	fmt.Printf("group:     %s\npriority:  %s\nstatus:    %s\n", task.Group, task.Priority, task.Status)
	if task.Due != "" {
		fmt.Printf("due:       %s\n", task.Due)
	}
	fmt.Printf("created:   %s\n", task.CreatedAt)
	if task.CompletedAt != nil {
		fmt.Printf("completed: %s\n", *task.CompletedAt)
	}
	if task.Notes != "" {
		fmt.Printf("notes:\n  %s\n", strings.ReplaceAll(task.Notes, "\n", "\n  "))
	}
}

func printHabitLine(habit Habit) {
	check := " "
	if habit.DoneToday {
		check = "x"
	}
	fmt.Printf("  %-4d [%s] %s  streak %d\n", habit.ID, check, habit.Title, habit.CurrentStreak)
}

func printHabitDetail(habit Habit) {
	fmt.Printf("#%d %s\n", habit.ID, habit.Title)
	fmt.Printf("done today:     %t\ncurrent streak: %d\nlongest streak: %d\ntotal days:     %d\n",
		habit.DoneToday, habit.CurrentStreak, habit.LongestStreak, habit.TotalDaysDone)
	fmt.Printf("done on:        %s\n", strings.Join(habit.CompletionDates, " "))
	if habit.Notes != "" {
		fmt.Printf("notes:\n  %s\n", strings.ReplaceAll(habit.Notes, "\n", "\n  "))
	}
}
