package command

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/jmlero/the-platform/cli/internal/appdeck"
)

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write JSON output: %w", err)
	}
	return nil
}

func writeTaskList(writer io.Writer, tasks []appdeck.Task) {
	if len(tasks) == 0 {
		_, _ = fmt.Fprintln(writer, "No tasks.")
		return
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "ID\tSTATUS\tPRIORITY\tTITLE")
	for _, task := range tasks {
		_, _ = fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\n",
			task.ID,
			task.Status,
			task.Priority,
			oneLine(task.Title),
		)
	}
	_ = table.Flush()
}

func writeTaskDetail(writer io.Writer, detail appdeck.TaskDetail) {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "ID:\t%s\n", detail.Task.ID)
	_, _ = fmt.Fprintf(table, "Project:\t%s\n", detail.Task.ProjectID)
	_, _ = fmt.Fprintf(table, "Title:\t%s\n", oneLine(detail.Task.Title))
	_, _ = fmt.Fprintf(table, "Status:\t%s\n", detail.Task.Status)
	_, _ = fmt.Fprintf(table, "Priority:\t%s\n", detail.Task.Priority)
	_, _ = fmt.Fprintf(table, "Budget:\t%d minutes\n", detail.Task.BudgetMinutes)
	_, _ = fmt.Fprintf(table, "Version:\t%d\n", detail.Task.Version)
	_, _ = fmt.Fprintf(table, "Description:\t%s\n", oneLine(detail.Task.Description))
	_, _ = fmt.Fprintf(table, "Acceptance:\t%s\n", oneLine(detail.Task.AcceptanceCriteria))
	_, _ = fmt.Fprintf(table, "Runs:\t%d\n", len(detail.Runs))
	_, _ = fmt.Fprintf(table, "Decisions:\t%d\n", len(detail.Decisions))
	_, _ = fmt.Fprintf(table, "Artifacts:\t%d\n", len(detail.Artifacts))
	_ = table.Flush()

	if len(detail.Runs) == 0 {
		return
	}
	_, _ = fmt.Fprintln(writer)
	runs := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(runs, "RUN\tSTATUS\tAGENT")
	for _, run := range detail.Runs {
		_, _ = fmt.Fprintf(runs, "%s\t%s\t%s\n", run.ID, run.Status, run.AgentID)
	}
	_ = runs.Flush()
}

func writeCommandResult(writer io.Writer, command appdeck.Command) {
	_, _ = fmt.Fprintf(
		writer,
		"Requested %s for %s: %s (sequence %d, %s)\n",
		command.Type,
		command.RunID,
		command.ID,
		command.Sequence,
		command.Status,
	)
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
