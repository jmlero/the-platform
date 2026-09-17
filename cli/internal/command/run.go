package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/jmlero/the-platform/cli/internal/deck"
)

const (
	defaultDeckURL    = "http://localhost:8080"
	defaultTimeout    = 10 * time.Second
	maxTokenFileBytes = 64 << 10
)

type runner struct {
	deckURL    string
	tokenFile  string
	timeout    time.Duration
	jsonOutput bool
	stdout     io.Writer
	stderr     io.Writer
	getenv     func(string) string
}

func Run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
	version string,
) int {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	for _, name := range []string{"APPDECK_URL", "APPDECK_TOKEN_FILE", "APPDECK_TOKEN"} {
		if getenv(name) != "" {
			_, _ = fmt.Fprintf(stderr, "rename environment variable %s to %s\n", name, strings.TrimPrefix(name, "APP"))
			return 2
		}
	}

	deckURL := strings.TrimSpace(getenv("DECK_URL"))
	if deckURL == "" {
		deckURL = defaultDeckURL
	}
	r := runner{
		deckURL:   deckURL,
		tokenFile: strings.TrimSpace(getenv("DECK_TOKEN_FILE")),
		timeout:   defaultTimeout,
		stdout:    stdout,
		stderr:    stderr,
		getenv:    getenv,
	}

	flags := flag.NewFlagSet("platformctl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&r.deckURL, "deck-url", r.deckURL, "Deck base URL")
	flags.StringVar(&r.tokenFile, "token-file", r.tokenFile, "file containing a Deck operator login token")
	flags.DurationVar(&r.timeout, "timeout", r.timeout, "request timeout")
	flags.BoolVar(&r.jsonOutput, "json", false, "write JSON output")
	showVersion := flags.Bool("version", false, "print version")
	flags.Usage = func() { writeRootUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "platformctl %s\n", version)
		return 0
	}
	if r.timeout <= 0 {
		return r.usageError("timeout must be greater than zero", writeRootUsage)
	}
	if flags.NArg() == 0 {
		return r.usageError("a command is required", writeRootUsage)
	}

	command := flags.Arg(0)
	commandArgs := flags.Args()[1:]
	switch command {
	case "health":
		if len(commandArgs) != 0 {
			return r.usageError("health does not accept arguments", writeRootUsage)
		}
		return r.runHealth(ctx)
	case "tasks":
		return r.runTasks(ctx, commandArgs)
	case "runs":
		return r.runRuns(ctx, commandArgs)
	case "help":
		writeRootUsage(stdout)
		return 0
	default:
		return r.usageError(fmt.Sprintf("unknown command %q", command), writeRootUsage)
	}
}

func (r runner) runRuns(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.usageError("a runs command is required", writeRunsUsage)
	}
	commandType := args[0]
	switch commandType {
	case "pause", "resume", "cancel", "replan", "retry":
		return r.runCreateCommand(ctx, commandType, args[1:])
	case "help":
		writeRunsUsage(r.stdout)
		return 0
	default:
		return r.usageError(fmt.Sprintf("unknown runs command %q", commandType), writeRunsUsage)
	}
}

func (r runner) runCreateCommand(ctx context.Context, commandType string, args []string) int {
	flags := flag.NewFlagSet("platformctl runs "+commandType, flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	reason := flags.String("reason", "", "reason for the operator command (required)")
	usage := func(writer io.Writer) { writeRunCommandUsage(writer, commandType) }
	flags.Usage = func() { usage(r.stderr) }
	if code := parseFlags(flags, args); code >= 0 {
		return code
	}
	if flags.NArg() != 1 {
		return r.usageError("runs "+commandType+" requires exactly one run ID", usage)
	}
	*reason = strings.TrimSpace(*reason)
	if *reason == "" {
		return r.usageError("reason is required", usage)
	}

	client, commandContext, cancel, err := r.client(ctx)
	if err != nil {
		return r.failure(err)
	}
	defer cancel()
	command, err := client.CreateCommand(commandContext, flags.Arg(0), deck.CreateCommandInput{
		Type: commandType, Reason: *reason,
	})
	if err != nil {
		return r.failure(err)
	}
	if r.jsonOutput {
		return r.writeJSON(command)
	}
	writeCommandResult(r.stdout, command)
	return 0
}

func (r runner) runHealth(ctx context.Context) int {
	client, commandContext, cancel, err := r.client(ctx)
	if err != nil {
		return r.failure(err)
	}
	defer cancel()
	health, err := client.Health(commandContext)
	if err != nil {
		return r.failure(err)
	}
	if r.jsonOutput {
		return r.writeJSON(health)
	}
	_, _ = fmt.Fprintf(r.stdout, "%s: %s (%s)\n", health.Service, health.Status, health.Time)
	return 0
}

func (r runner) runTasks(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.usageError("a tasks command is required", writeTasksUsage)
	}
	switch args[0] {
	case "list":
		return r.runTasksList(ctx, args[1:])
	case "get":
		return r.runTasksGet(ctx, args[1:])
	case "create":
		return r.runTasksCreate(ctx, args[1:])
	case "help":
		writeTasksUsage(r.stdout)
		return 0
	default:
		return r.usageError(fmt.Sprintf("unknown tasks command %q", args[0]), writeTasksUsage)
	}
}

func (r runner) runTasksList(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl tasks list", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	status := flags.String("status", "", "filter by task status")
	project := flags.String("project", "", "filter by project ID")
	flags.Usage = func() { writeTasksListUsage(r.stderr) }
	if code := parseFlags(flags, args); code >= 0 {
		return code
	}
	if flags.NArg() != 0 {
		return r.usageError("tasks list does not accept positional arguments", writeTasksListUsage)
	}
	if *status != "" && !validStatus(*status) {
		return r.usageError(fmt.Sprintf("unknown task status %q", *status), writeTasksListUsage)
	}

	client, commandContext, cancel, err := r.client(ctx)
	if err != nil {
		return r.failure(err)
	}
	defer cancel()
	bootstrap, err := client.Bootstrap(commandContext)
	if err != nil {
		return r.failure(err)
	}
	tasks := filterTasks(bootstrap.Tasks, strings.TrimSpace(*status), strings.TrimSpace(*project))
	if r.jsonOutput {
		return r.writeJSON(tasks)
	}
	writeTaskList(r.stdout, tasks)
	return 0
}

func (r runner) runTasksGet(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl tasks get", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	flags.Usage = func() { writeTasksGetUsage(r.stderr) }
	if code := parseFlags(flags, args); code >= 0 {
		return code
	}
	if flags.NArg() != 1 {
		return r.usageError("tasks get requires exactly one task ID", writeTasksGetUsage)
	}

	client, commandContext, cancel, err := r.client(ctx)
	if err != nil {
		return r.failure(err)
	}
	defer cancel()
	detail, err := client.Task(commandContext, flags.Arg(0))
	if err != nil {
		return r.failure(err)
	}
	if r.jsonOutput {
		return r.writeJSON(detail)
	}
	writeTaskDetail(r.stdout, detail)
	return 0
}

func (r runner) runTasksCreate(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl tasks create", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	input := deck.CreateTaskInput{}
	flags.StringVar(&input.ProjectID, "project", "prj_default", "project ID")
	flags.StringVar(&input.Title, "title", "", "task title (required)")
	flags.StringVar(&input.Description, "description", "", "task description")
	flags.StringVar(&input.AcceptanceCriteria, "acceptance", "", "acceptance criteria")
	flags.StringVar(&input.Status, "status", "backlog", "initial status: backlog or ready")
	flags.StringVar(&input.Priority, "priority", "medium", "priority: low, medium, high, or urgent")
	flags.IntVar(&input.BudgetMinutes, "budget-minutes", 30, "execution budget in minutes")
	flags.Usage = func() { writeTasksCreateUsage(r.stderr) }
	if code := parseFlags(flags, args); code >= 0 {
		return code
	}
	if flags.NArg() != 0 {
		return r.usageError("tasks create does not accept positional arguments", writeTasksCreateUsage)
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.AcceptanceCriteria = strings.TrimSpace(input.AcceptanceCriteria)
	input.Status = strings.TrimSpace(input.Status)
	input.Priority = strings.TrimSpace(input.Priority)
	if input.Title == "" {
		return r.usageError("title is required", writeTasksCreateUsage)
	}
	if input.ProjectID == "" {
		return r.usageError("project ID is required", writeTasksCreateUsage)
	}
	if input.Status != "backlog" && input.Status != "ready" {
		return r.usageError("initial status must be backlog or ready", writeTasksCreateUsage)
	}
	if !validPriority(input.Priority) {
		return r.usageError(fmt.Sprintf("unknown priority %q", input.Priority), writeTasksCreateUsage)
	}
	if input.BudgetMinutes <= 0 {
		return r.usageError("budget-minutes must be greater than zero", writeTasksCreateUsage)
	}

	client, commandContext, cancel, err := r.client(ctx)
	if err != nil {
		return r.failure(err)
	}
	defer cancel()
	task, err := client.CreateTask(commandContext, input)
	if err != nil {
		return r.failure(err)
	}
	if r.jsonOutput {
		return r.writeJSON(task)
	}
	_, _ = fmt.Fprintf(r.stdout, "Created %s [%s] %s\n", task.ID, task.Status, oneLine(task.Title))
	return 0
}

func (r runner) client(ctx context.Context) (*deck.Client, context.Context, context.CancelFunc, error) {
	token, err := r.token()
	if err != nil {
		return nil, nil, nil, err
	}
	httpClient := &http.Client{Timeout: r.timeout}
	client, err := deck.New(r.deckURL, token, httpClient)
	if err != nil {
		return nil, nil, nil, err
	}
	commandContext, cancel := context.WithTimeout(ctx, r.timeout)
	cleanup := func() {
		defer cancel()
		logoutContext, stop := context.WithTimeout(context.Background(), min(r.timeout, 2*time.Second))
		defer stop()
		_ = client.Close(logoutContext)
	}
	return client, commandContext, cleanup, nil
}

func (r runner) token() (string, error) {
	if strings.TrimSpace(r.tokenFile) == "" {
		return strings.TrimSpace(r.getenv("DECK_TOKEN")), nil
	}
	file, err := os.Open(r.tokenFile)
	if err != nil {
		return "", fmt.Errorf("open token file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("token file must be accessible only to its owner (chmod 600)")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxTokenFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	if len(contents) > maxTokenFileBytes {
		return "", fmt.Errorf("token file exceeds %d bytes", maxTokenFileBytes)
	}
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", fmt.Errorf("token file is empty")
	}
	return token, nil
}

func (r runner) writeJSON(value any) int {
	if err := writeJSON(r.stdout, value); err != nil {
		return r.failure(err)
	}
	return 0
}

func (r runner) usageError(message string, usage func(io.Writer)) int {
	_, _ = fmt.Fprintf(r.stderr, "platformctl: %s\n\n", message)
	usage(r.stderr)
	return 2
}

func (r runner) failure(err error) int {
	_, _ = fmt.Fprintf(r.stderr, "platformctl: %v\n", err)
	return 1
}

func parseFlags(flags *flag.FlagSet, args []string) int {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	return -1
}

func filterTasks(tasks []deck.Task, status, project string) []deck.Task {
	filtered := make([]deck.Task, 0, len(tasks))
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		if project != "" && task.ProjectID != project {
			continue
		}
		filtered = append(filtered, task)
	}
	return filtered
}

func validStatus(value string) bool {
	switch value {
	case "backlog", "ready", "planning", "waiting_decision", "running", "verifying", "done", "blocked", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func validPriority(value string) bool {
	switch value {
	case "low", "medium", "high", "urgent":
		return true
	default:
		return false
	}
}

func writeRootUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] health
  platformctl [global options] tasks <command> [options]
  platformctl [global options] runs <command> [options] <run-id>

Commands:
  health          Verify that Deck is reachable
  tasks list      List work from the operator workspace
  tasks get       Inspect a task and its runs
  tasks create    Create backlog or ready work
  runs pause      Request a durable pause command
  runs resume     Request a durable resume command
  runs cancel     Request a durable cancel command
  runs replan     Request a durable replan command
  runs retry      Request a durable retry command

Global options:
  --deck-url URL    Deck base URL (default: DECK_URL or http://localhost:8080)
  --token-file PATH    Operator-token file (default: DECK_TOKEN_FILE)
  --timeout DURATION   Request timeout (default: 10s)
  --json               Write JSON output
  --version            Print version

Run "platformctl tasks help" or "platformctl runs help" for more.`)
}

func writeTasksUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] tasks list [options]
  platformctl [global options] tasks get <task-id>
  platformctl [global options] tasks create [options]`)
}

func writeTasksListUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] tasks list [--status STATUS] [--project PROJECT_ID]`)
}

func writeTasksGetUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] tasks get <task-id>`)
}

func writeTasksCreateUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] tasks create --title TITLE [options]

Options:
  --project ID             Project ID (default: prj_default)
  --description TEXT       Task description
  --acceptance TEXT        Acceptance criteria
  --status STATUS          backlog or ready (default: backlog)
  --priority PRIORITY      low, medium, high, or urgent (default: medium)
  --budget-minutes NUMBER  Execution budget (default: 30)`)
}

func writeRunsUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] runs pause --reason REASON <run-id>
  platformctl [global options] runs resume --reason REASON <run-id>
  platformctl [global options] runs cancel --reason REASON <run-id>
  platformctl [global options] runs replan --reason REASON <run-id>
  platformctl [global options] runs retry --reason REASON <run-id>`)
}

func writeRunCommandUsage(writer io.Writer, commandType string) {
	_, _ = fmt.Fprintf(writer, `Usage:
  platformctl [global options] runs %s --reason REASON <run-id>
`, commandType)
}
