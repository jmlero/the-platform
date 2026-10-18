package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/jmlero/the-platform/cli/internal/marina"
)

const (
	defaultMarinaURL       = "http://localhost:8090"
	defaultMarinaNamespace = "default"
	maxCAFileBytes         = 1 << 20
	maxContextBytes        = 256 << 10
	maxContextItems        = 32
)

// marinaFollowPollInterval is how often "runs events --follow" checks the
// Run's phase. Tests shorten it.
var marinaFollowPollInterval = 3 * time.Second

func (r runner) marinaClient() (*marina.Client, error) {
	if !marina.ValidName(r.namespace) {
		return nil, fmt.Errorf("namespace %q is not a valid name", r.namespace)
	}
	token := strings.TrimSpace(r.getenv("MARINA_TOKEN"))
	if strings.TrimSpace(r.marinaTokenFile) != "" {
		var err error
		if token, err = readTokenFile(r.marinaTokenFile); err != nil {
			return nil, err
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = r.timeout
	if strings.TrimSpace(r.marinaCAFile) != "" {
		pool, err := r.marinaCertPool()
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return marina.New(r.marinaURL, token, &http.Client{Transport: transport})
}

// marinaCertPool adds the installation CA bundle to the system roots.
func (r runner) marinaCertPool() (*x509.CertPool, error) {
	file, err := os.Open(r.marinaCAFile)
	if err != nil {
		return nil, fmt.Errorf("open CA file: %w", err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxCAFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	if len(contents) > maxCAFileBytes {
		return nil, fmt.Errorf("CA file exceeds %d bytes", maxCAFileBytes)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("CA file contains no PEM certificates")
	}
	return pool, nil
}

func (r runner) withMarina(ctx context.Context, call func(context.Context, *marina.Client) int) int {
	client, err := r.marinaClient()
	if err != nil {
		return r.failure(err)
	}
	requestContext, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return call(requestContext, client)
}

func (r runner) writeRaw(raw json.RawMessage) int {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, raw, "", "  "); err != nil {
		return r.failure(fmt.Errorf("write JSON output: %w", err))
	}
	formatted.WriteByte('\n')
	if _, err := r.stdout.Write(formatted.Bytes()); err != nil {
		return r.failure(fmt.Errorf("write JSON output: %w", err))
	}
	return 0
}

// parseInterspersed allows flags before and after positional arguments, as in
// "runs create NAME --project P".
func parseInterspersed(flags *flag.FlagSet, args []string) ([]string, int) {
	var positional []string
	for {
		if code := parseFlags(flags, args); code >= 0 {
			return nil, code
		}
		rest := flags.Args()
		if len(rest) == 0 {
			return positional, -1
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), -1
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func (r runner) parseNamed(flags *flag.FlagSet, args []string, count int, usage func(io.Writer)) ([]string, int) {
	flags.SetOutput(r.stderr)
	flags.Usage = func() { usage(r.stderr) }
	positional, code := parseInterspersed(flags, args)
	if code >= 0 {
		return nil, code
	}
	if len(positional) != count {
		return nil, r.usageError(fmt.Sprintf("%s requires %d argument(s)", strings.TrimPrefix(flags.Name(), "platformctl "), count), usage)
	}
	if !marina.ValidName(positional[0]) {
		return nil, r.usageError(fmt.Sprintf("name %q must be lowercase letters, digits and dashes (at most 63)", positional[0]), usage)
	}
	return positional, -1
}

func idempotencyKey() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (r runner) runCapabilities(ctx context.Context, args []string) int {
	if len(args) != 0 {
		return r.usageError("capabilities does not accept arguments", writeRootUsage)
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		capabilities, err := client.Capabilities(ctx)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(capabilities.Raw)
		}
		writeCapabilities(r.stdout, capabilities)
		return 0
	})
}

type contextFlags []string

func (c *contextFlags) String() string     { return strings.Join(*c, ",") }
func (c *contextFlags) Set(v string) error { *c = append(*c, v); return nil }

// readContextItem reads NAME=PATH as data-trust document context.
func readContextItem(value string) (marina.ContextItem, error) {
	name, path, ok := strings.Cut(value, "=")
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if !ok || name == "" || path == "" {
		return marina.ContextItem{}, fmt.Errorf("context %q must be NAME=PATH", value)
	}
	if len(name) > 200 {
		return marina.ContextItem{}, fmt.Errorf("context name %q exceeds 200 characters", name)
	}
	file, err := os.Open(path)
	if err != nil {
		return marina.ContextItem{}, fmt.Errorf("open context file: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxContextBytes+1))
	if err != nil {
		return marina.ContextItem{}, fmt.Errorf("read context file: %w", err)
	}
	if len(content) > maxContextBytes {
		return marina.ContextItem{}, fmt.Errorf("context file %s exceeds %d bytes", path, maxContextBytes)
	}
	if !utf8.Valid(content) {
		return marina.ContextItem{}, fmt.Errorf("context file %s is not UTF-8 text", path)
	}
	digest := sha256.Sum256(content)
	return marina.ContextItem{
		Name: name, Kind: "document", Trust: "data",
		Digest: "sha256:" + hex.EncodeToString(digest[:]), Content: string(content),
	}, nil
}

func (r runner) runMarinaRunCreate(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl runs create", flag.ContinueOnError)
	spec := marina.RunSpec{}
	var contexts contextFlags
	flags.StringVar(&spec.Project, "project", "", "project name (required)")
	flags.StringVar(&spec.Workload, "workload", "", "workload name (default: the project's)")
	flags.StringVar(&spec.Title, "title", "", "run title (required)")
	flags.StringVar(&spec.AcceptanceCriteria, "criteria", "", "acceptance criteria (required)")
	flags.StringVar(&spec.Description, "description", "", "run description")
	flags.IntVar(&spec.Budget.TimeMinutes, "minutes", 0, "time budget in minutes (required)")
	flags.Int64Var(&spec.Budget.MaxTokens, "tokens", 0, "token budget (required)")
	flags.IntVar(&spec.Budget.MaxAttempts, "attempts", 0, "attempt budget (required)")
	flags.Var(&contexts, "context", "NAME=PATH of a document sent as data (repeatable)")
	positional, code := r.parseNamed(flags, args, 1, writeMarinaRunCreateUsage)
	if code >= 0 {
		return code
	}
	spec.Project, spec.Workload = strings.TrimSpace(spec.Project), strings.TrimSpace(spec.Workload)
	spec.Title, spec.AcceptanceCriteria = strings.TrimSpace(spec.Title), strings.TrimSpace(spec.AcceptanceCriteria)
	spec.Description = strings.TrimSpace(spec.Description)
	switch {
	case !marina.ValidName(spec.Project):
		return r.usageError("a valid --project name is required", writeMarinaRunCreateUsage)
	case spec.Workload != "" && !marina.ValidName(spec.Workload):
		return r.usageError("--workload must be a valid name", writeMarinaRunCreateUsage)
	case spec.Title == "":
		return r.usageError("--title is required", writeMarinaRunCreateUsage)
	case spec.AcceptanceCriteria == "":
		return r.usageError("--criteria is required", writeMarinaRunCreateUsage)
	case spec.Budget.TimeMinutes <= 0 || spec.Budget.MaxTokens <= 0 || spec.Budget.MaxAttempts <= 0:
		return r.usageError("--minutes, --tokens and --attempts must be greater than zero", writeMarinaRunCreateUsage)
	case len(contexts) > maxContextItems:
		return r.usageError(fmt.Sprintf("at most %d --context items are allowed", maxContextItems), writeMarinaRunCreateUsage)
	}
	for _, value := range contexts {
		item, err := readContextItem(value)
		if err != nil {
			return r.usageError(err.Error(), writeMarinaRunCreateUsage)
		}
		spec.Context = append(spec.Context, item)
	}

	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		run, created, err := client.CreateRun(ctx, r.namespace, positional[0], spec)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(run.Raw)
		}
		if created {
			_, _ = fmt.Fprintf(r.stdout, "Created run %s (%s)\n", run.Metadata.Name, phase(run.Status.Phase))
		} else {
			_, _ = fmt.Fprintf(r.stdout, "Run %s already exists with the same spec (%s)\n", run.Metadata.Name, phase(run.Status.Phase))
		}
		return 0
	})
}

func (r runner) runMarinaRunList(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl runs list", flag.ContinueOnError)
	flags.SetOutput(r.stderr)
	flags.Usage = func() { writeRunsUsage(r.stderr) }
	if code := parseFlags(flags, args); code >= 0 {
		return code
	}
	if flags.NArg() != 0 {
		return r.usageError("runs list does not accept arguments", writeRunsUsage)
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		runs, err := client.ListRuns(ctx, r.namespace)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(runs.Raw)
		}
		writeRunTable(r.stdout, runs.Items)
		return 0
	})
}

func (r runner) runMarinaRunGet(ctx context.Context, args []string) int {
	positional, code := r.parseNamed(flag.NewFlagSet("platformctl runs get", flag.ContinueOnError), args, 1, writeRunsUsage)
	if code >= 0 {
		return code
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		run, err := client.GetRun(ctx, r.namespace, positional[0])
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(run.Raw)
		}
		writeRunDetail(r.stdout, run)
		return 0
	})
}

func (r runner) runMarinaRunCancel(ctx context.Context, args []string) int {
	positional, code := r.parseNamed(flag.NewFlagSet("platformctl runs cancel", flag.ContinueOnError), args, 1, writeRunsUsage)
	if code >= 0 {
		return code
	}
	key, err := idempotencyKey()
	if err != nil {
		return r.failure(err)
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		run, err := client.CancelRun(ctx, r.namespace, positional[0], key)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(run.Raw)
		}
		_, _ = fmt.Fprintf(r.stdout, "Cancel requested for run %s (%s)\n", run.Metadata.Name, phase(run.Status.Phase))
		return 0
	})
}

func (r runner) runMarinaRunEvents(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl runs events", flag.ContinueOnError)
	after := flags.Int64("after", 0, "show events after this sequence")
	follow := flags.Bool("follow", false, "stream events until the run ends or Ctrl-C")
	positional, code := r.parseNamed(flags, args, 1, writeRunsUsage)
	if code >= 0 {
		return code
	}
	if *after < 0 {
		return r.usageError("--after must not be negative", writeRunsUsage)
	}
	if *follow {
		client, err := r.marinaClient()
		if err != nil {
			return r.failure(err)
		}
		return r.followRunEvents(ctx, client, positional[0], *after)
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		page, err := client.RunEvents(ctx, r.namespace, positional[0], *after)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(page.Raw)
		}
		for _, event := range page.Items {
			r.writeEvent(event)
		}
		return 0
	})
}

func terminalPhase(value string) bool {
	switch value {
	case "Succeeded", "Blocked", "Failed", "Cancelled":
		return true
	default:
		return false
	}
}

// followRunEvents streams events and stops on interruption or once the Run is
// terminal, after reading any events still unread.
func (r runner) followRunEvents(ctx context.Context, client *marina.Client, name string, after int64) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	show := func(event marina.Event) error {
		if event.Sequence > after {
			r.writeEvent(event)
			after = event.Sequence
		}
		return nil
	}
	for {
		streamContext, cancelStream := context.WithCancel(ctx)
		var final marina.Run
		polled := make(chan error, 1)
		go func() {
			defer cancelStream()
			run, err := r.waitForTerminalRun(streamContext, client, name)
			final = run
			polled <- err
		}()
		streamErr := client.StreamRunEvents(streamContext, r.namespace, name, after, show)
		cancelStream()
		pollErr := <-polled
		switch {
		case ctx.Err() != nil:
			return 0
		case pollErr == nil:
			return r.finishFollow(ctx, client, final, &after, show)
		case !errors.Is(pollErr, context.Canceled):
			return r.failure(pollErr)
		case streamErr != nil && !errors.Is(streamErr, context.Canceled):
			return r.failure(streamErr)
		}
		// The stream ended while the Run was still active: reconnect.
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(marinaFollowPollInterval):
		}
	}
}

func (r runner) waitForTerminalRun(ctx context.Context, client *marina.Client, name string) (marina.Run, error) {
	ticker := time.NewTicker(marinaFollowPollInterval)
	defer ticker.Stop()
	for {
		requestContext, cancel := context.WithTimeout(ctx, r.timeout)
		run, err := client.GetRun(requestContext, r.namespace, name)
		cancel()
		if ctx.Err() != nil {
			return marina.Run{}, ctx.Err()
		}
		if err != nil {
			return marina.Run{}, err
		}
		if terminalPhase(run.Status.Phase) {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return marina.Run{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r runner) finishFollow(ctx context.Context, client *marina.Client, run marina.Run, after *int64, show func(marina.Event) error) int {
	for {
		requestContext, cancel := context.WithTimeout(ctx, r.timeout)
		page, err := client.RunEvents(requestContext, r.namespace, run.Metadata.Name, *after)
		cancel()
		if err != nil {
			return r.failure(err)
		}
		previous := *after
		for _, event := range page.Items {
			_ = show(event)
		}
		if *after == previous {
			break
		}
	}
	if !r.jsonOutput {
		_, _ = fmt.Fprintf(r.stdout, "Run %s is %s\n", run.Metadata.Name, run.Status.Phase)
	}
	return 0
}

func (r runner) writeEvent(event marina.Event) {
	if r.jsonOutput {
		encoded, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(r.stdout, "%s\n", encoded)
		return
	}
	_, _ = fmt.Fprintf(r.stdout, "%d %s %s %s\n", event.Sequence, event.Time, event.Type, oneLine(event.Message))
}

func (r runner) runTurns(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.usageError("a turns command is required", writeTurnsUsage)
	}
	switch args[0] {
	case "create":
		return r.runTurnCreate(ctx, args[1:])
	case "get":
		return r.runTurnGet(ctx, args[1:])
	case "help":
		writeTurnsUsage(r.stdout)
		return 0
	default:
		return r.usageError(fmt.Sprintf("unknown turns command %q", args[0]), writeTurnsUsage)
	}
}

func (r runner) runTurnCreate(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("platformctl turns create", flag.ContinueOnError)
	spec := marina.TurnSpec{}
	var message string
	flags.StringVar(&spec.Project, "project", "", "project name (required)")
	flags.StringVar(&spec.Workload, "workload", "", "workload name (default: the project's)")
	flags.StringVar(&message, "message", "", "the human message (required)")
	flags.IntVar(&spec.Budget.TimeMinutes, "minutes", 15, "time budget in minutes")
	flags.Int64Var(&spec.Budget.MaxTokens, "tokens", 40000, "token budget")
	positional, code := r.parseNamed(flags, args, 1, writeTurnsUsage)
	if code >= 0 {
		return code
	}
	spec.Project, spec.Workload = strings.TrimSpace(spec.Project), strings.TrimSpace(spec.Workload)
	message = strings.TrimSpace(message)
	switch {
	case !marina.ValidName(spec.Project):
		return r.usageError("a valid --project name is required", writeTurnsUsage)
	case spec.Workload != "" && !marina.ValidName(spec.Workload):
		return r.usageError("--workload must be a valid name", writeTurnsUsage)
	case message == "":
		return r.usageError("--message is required", writeTurnsUsage)
	case spec.Budget.TimeMinutes <= 0 || spec.Budget.MaxTokens <= 0:
		return r.usageError("--minutes and --tokens must be greater than zero", writeTurnsUsage)
	}
	// Thread and message IDs derive from the Turn name so a retried command
	// replays the same spec instead of conflicting with the first attempt.
	spec.Thread.ID = "thr-" + positional[0]
	spec.Thread.Messages = []marina.TurnMessage{{ID: "msg-1", Role: "human", Body: message}}

	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		turn, created, err := client.CreateTurn(ctx, r.namespace, positional[0], spec)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(turn.Raw)
		}
		if created {
			_, _ = fmt.Fprintf(r.stdout, "Created turn %s (%s)\n", turn.Metadata.Name, phase(turn.Status.Phase))
		} else {
			_, _ = fmt.Fprintf(r.stdout, "Turn %s already exists with the same spec (%s)\n", turn.Metadata.Name, phase(turn.Status.Phase))
		}
		return 0
	})
}

func (r runner) runTurnGet(ctx context.Context, args []string) int {
	positional, code := r.parseNamed(flag.NewFlagSet("platformctl turns get", flag.ContinueOnError), args, 1, writeTurnsUsage)
	if code >= 0 {
		return code
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		turn, err := client.GetTurn(ctx, r.namespace, positional[0])
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(turn.Raw)
		}
		writeTurnDetail(r.stdout, turn)
		return 0
	})
}

func (r runner) runDecisions(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.usageError("a decisions command is required", writeDecisionsUsage)
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return r.usageError("decisions list does not accept arguments", writeDecisionsUsage)
		}
		return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
			decisions, err := client.ListDecisions(ctx, r.namespace)
			if err != nil {
				return r.failure(err)
			}
			if r.jsonOutput {
				return r.writeRaw(decisions.Raw)
			}
			writeDecisionTable(r.stdout, decisions.Items)
			return 0
		})
	case "answer":
		return r.runDecisionAnswer(ctx, args[1:])
	case "help":
		writeDecisionsUsage(r.stdout)
		return 0
	default:
		return r.usageError(fmt.Sprintf("unknown decisions command %q", args[0]), writeDecisionsUsage)
	}
}

func (r runner) runDecisionAnswer(ctx context.Context, args []string) int {
	positional, code := r.parseNamed(flag.NewFlagSet("platformctl decisions answer", flag.ContinueOnError), args, 2, writeDecisionsUsage)
	if code >= 0 {
		return code
	}
	answer := strings.TrimSpace(positional[1])
	if answer == "" {
		return r.usageError("an answer is required", writeDecisionsUsage)
	}
	key, err := idempotencyKey()
	if err != nil {
		return r.failure(err)
	}
	return r.withMarina(ctx, func(ctx context.Context, client *marina.Client) int {
		decision, err := client.AnswerDecision(ctx, r.namespace, positional[0], answer, key)
		if err != nil {
			return r.failure(err)
		}
		if r.jsonOutput {
			return r.writeRaw(decision.Raw)
		}
		_, _ = fmt.Fprintf(r.stdout, "Answered decision %s: %s (%s)\n", decision.Metadata.Name, oneLine(decision.Spec.Response.Answer), phase(decision.Status.Phase))
		return 0
	})
}

func phase(value string) string {
	if value == "" {
		return "no status yet"
	}
	return value
}

func runResult(run marina.Run) string {
	switch {
	case run.Status.Landing.Commit != "":
		return run.Status.Landing.Commit
	case run.Status.PullRequest.URL != "":
		return run.Status.PullRequest.URL
	default:
		return oneLine(run.Status.Reason)
	}
}

func writeCapabilities(writer io.Writer, capabilities marina.Capabilities) {
	_, _ = fmt.Fprintf(writer, "Workload types: %s\n", strings.Join(capabilities.WorkloadTypes, ", "))
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "Models:")
	for _, model := range capabilities.Models {
		_, _ = fmt.Fprintf(table, "  %s\t%s\n", model.Name, strings.Join(model.Roles, ", "))
	}
	_, _ = fmt.Fprintln(table, "Tools:")
	for _, tool := range capabilities.Tools {
		_, _ = fmt.Fprintf(table, "  %s\t%s\n", tool.Name, oneLine(tool.Description))
	}
	_ = table.Flush()
	limits := capabilities.Limits
	_, _ = fmt.Fprintf(writer, "Limits: %d minutes, %d tokens, %d attempts\n", limits.MaxTimeMinutes, limits.MaxTokens, limits.MaxAttempts)
}

func writeRunTable(writer io.Writer, runs []marina.Run) {
	if len(runs) == 0 {
		_, _ = fmt.Fprintln(writer, "No runs.")
		return
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "NAME\tPROJECT\tPHASE\tTOKENS\tRESULT")
	for _, run := range runs {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%s\n", run.Metadata.Name, run.Spec.Project, phase(run.Status.Phase), run.Status.Usage.Tokens, runResult(run))
	}
	_ = table.Flush()
}

func writeRunDetail(writer io.Writer, run marina.Run) {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "Name:\t%s\n", run.Metadata.Name)
	_, _ = fmt.Fprintf(table, "Project:\t%s\n", run.Spec.Project)
	_, _ = fmt.Fprintf(table, "Workload:\t%s\n", run.Spec.Workload)
	_, _ = fmt.Fprintf(table, "Title:\t%s\n", oneLine(run.Spec.Title))
	_, _ = fmt.Fprintf(table, "Phase:\t%s\n", phase(run.Status.Phase))
	if run.Status.Reason != "" {
		_, _ = fmt.Fprintf(table, "Reason:\t%s\n", oneLine(run.Status.Reason))
	}
	_, _ = fmt.Fprintf(table, "Tokens:\t%d of %d\n", run.Status.Usage.Tokens, run.Spec.Budget.MaxTokens)
	_, _ = fmt.Fprintf(table, "Attempts:\t%d/%d\n", run.Status.Usage.Attempts, run.Spec.Budget.MaxAttempts)
	if run.Status.Landing.Commit != "" {
		_, _ = fmt.Fprintf(table, "Landed:\t%s\n", run.Status.Landing.Commit)
	}
	if run.Status.PullRequest.URL != "" {
		_, _ = fmt.Fprintf(table, "Pull request:\t%s\n", run.Status.PullRequest.URL)
	}
	if len(run.Status.Decisions) > 0 {
		_, _ = fmt.Fprintf(table, "Decisions:\t%s\n", strings.Join(run.Status.Decisions, ", "))
	}
	_ = table.Flush()
	if len(run.Status.Evidence) == 0 {
		return
	}
	_, _ = fmt.Fprintln(writer)
	evidence := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(evidence, "EVIDENCE\tPASSED\tREVISION\tSUMMARY")
	for _, item := range run.Status.Evidence {
		_, _ = fmt.Fprintf(evidence, "%s\t%t\t%s\t%s\n", item.Kind, item.Passed, item.SourceRevision, oneLine(item.Summary))
	}
	_ = evidence.Flush()
}

func writeTurnDetail(writer io.Writer, turn marina.Turn) {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "Name:\t%s\n", turn.Metadata.Name)
	_, _ = fmt.Fprintf(table, "Project:\t%s\n", turn.Spec.Project)
	_, _ = fmt.Fprintf(table, "Phase:\t%s\n", phase(turn.Status.Phase))
	if turn.Status.Reason != "" {
		_, _ = fmt.Fprintf(table, "Reason:\t%s\n", oneLine(turn.Status.Reason))
	}
	_, _ = fmt.Fprintf(table, "Tokens:\t%d\n", turn.Status.Usage.Tokens)
	if len(turn.Status.Decisions) > 0 {
		_, _ = fmt.Fprintf(table, "Decisions:\t%s\n", strings.Join(turn.Status.Decisions, ", "))
	}
	_ = table.Flush()
	if turn.Status.Reply.Answer != "" {
		_, _ = fmt.Fprintf(writer, "\nReply:\n%s\n", strings.TrimRight(turn.Status.Reply.Answer, "\n"))
	}
}

func writeDecisionTable(writer io.Writer, decisions []marina.Decision) {
	if len(decisions) == 0 {
		_, _ = fmt.Fprintln(writer, "No decisions.")
		return
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "NAME\tOWNER\tKIND\tPHASE\tQUESTION")
	for _, decision := range decisions {
		owner := decision.Spec.Owner.Kind + "/" + decision.Spec.Owner.Name
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", decision.Metadata.Name, owner, decision.Spec.Kind, phase(decision.Status.Phase), oneLine(decision.Spec.Question))
	}
	_ = table.Flush()
}

func writeMarinaRunCreateUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] runs create NAME --project PROJECT --title TITLE
      --criteria CRITERIA --minutes N --tokens N --attempts N [options]

Options:
  --workload NAME        Workload (default: the project's default)
  --description TEXT     Run description
  --context NAME=PATH    Send a local file as data context (repeatable)`)
}

func writeTurnsUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] turns create NAME --project PROJECT --message TEXT
      [--workload NAME] [--minutes N (default 15)] [--tokens N (default 40000)]
  platformctl [global options] turns get NAME`)
}

func writeDecisionsUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage:
  platformctl [global options] decisions list
  platformctl [global options] decisions answer NAME ANSWER`)
}
