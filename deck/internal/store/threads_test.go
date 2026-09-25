package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestThreadSurvivesRestartAndExportWithTaskLinksAndOrderedReplies(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "threads.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ch, err := s.CreateChannel(ctx, "prj_default", "work")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, AuthorType: "human", AuthorID: "operator", Body: "Investigate login"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.CreateThread(ctx, ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateThread(ctx, ch.ID, root.ID)
	if err != nil || again.ID != thread.ID {
		t.Fatalf("replay: %#v %v", again, err)
	}
	reply, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, ThreadID: thread.ID, AuthorType: "human", AuthorID: "operator", Body: "Use GitHub"})
	if err != nil {
		t.Fatal(err)
	}
	budget := domain.Budget{TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 10000, MaxCostMinor: 100, Currency: "EUR", MaxConcurrency: 1, Authority: []string{"source.read"}}
	task, err := s.PromoteChannelMessage(ctx, reply.ID, domain.CreateTaskInput{Title: "Investigate", AcceptanceCriteria: "Report cause", Status: domain.TaskBacklog, Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := s.GetTaskThread(ctx, task.ID)
	if err != nil || linked.ID != thread.ID || len(linked.TaskIDs) != 1 || linked.TaskIDs[0] != task.ID {
		t.Fatalf("origin: %#v %v", linked, err)
	}
	repeated, err := s.PromoteChannelMessage(ctx, reply.ID, domain.CreateTaskInput{})
	if err != nil || repeated.ID != task.ID {
		t.Fatalf("promotion replay: %#v %v", repeated, err)
	}
	if err := s.DeleteDraft(ctx, task.ID, task.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("delete linked task: %v", err)
	}
	if _, err := s.SetTaskArchived(ctx, task.ID, task.Version, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	linked, err = s.GetTaskThread(ctx, task.ID)
	if err != nil || linked.ID != thread.ID {
		t.Fatalf("reopened origin: %#v %v", linked, err)
	}
	page, err := s.PageThreadMessages(ctx, thread.ID, 1, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != root.ID || page.NextCursor == "" {
		t.Fatalf("first page: %#v %v", page, err)
	}
	cursor := page.NextCursor
	page, err = s.PageThreadMessages(ctx, thread.ID, 1, cursor)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != reply.ID || page.Items[0].ThreadID != thread.ID || page.NextCursor != "" {
		t.Fatalf("second page: %#v %v", page, err)
	}
	exported, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.ImportProject(ctx, exported); err != nil {
		t.Fatal(err)
	}
	linked, err = restored.GetTaskThread(ctx, task.ID)
	if err != nil || linked.ID != thread.ID {
		t.Fatalf("restored origin: %#v %v", linked, err)
	}
	page, err = restored.PageThreadMessages(ctx, thread.ID, 1, cursor)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != reply.ID {
		t.Fatalf("restored cursor: %#v %v", page, err)
	}
	reexport, err := restored.ExportProject(ctx, "prj_default")
	if err != nil || !bytes.Equal(exported, reexport) {
		t.Fatalf("export round trip differs: %v", err)
	}
}

func TestThreadRejectsForeignMessagesTasksAndCursors(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "boundaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.ExecContext(ctx, "INSERT INTO projects (id,name,slug,description,created_at) VALUES ('other','Other','other','',1)"); err != nil {
		t.Fatal(err)
	}
	ch, _ := s.CreateChannel(ctx, "prj_default", "main")
	other, _ := s.CreateChannel(ctx, "other", "other")
	root, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, AuthorType: "human", AuthorID: "operator", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.CreateThread(ctx, ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateThread(ctx, other.ID, root.ID); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("foreign root: %v", err)
	}
	if _, err = s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: other.ID, ThreadID: thread.ID, AuthorType: "human", AuthorID: "operator", Body: "bad"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("foreign reply: %v", err)
	}
	foreign, err := s.CreateTask(ctx, domain.CreateTaskInput{ProjectID: "other", Title: "foreign", Status: domain.TaskBacklog})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkThreadTask(ctx, thread.ID, foreign.ID); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("foreign task: %v", err)
	}
	local, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "local", Status: domain.TaskBacklog})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.LinkThreadTask(ctx, thread.ID, local.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.DeleteDraft(ctx, local.ID, local.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("delete linked draft: %v", err)
	}
	secondRoot, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, AuthorType: "human", AuthorID: "operator", Body: "second"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateThread(ctx, ch.ID, secondRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkThreadTask(ctx, second.ID, local.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("reassign origin: %v", err)
	}
	_, err = s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, ThreadID: thread.ID, AuthorType: "human", AuthorID: "operator", Body: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.PageThreadMessages(ctx, thread.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PageThreadMessages(ctx, second.ID, 1, page.NextCursor); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("foreign cursor: %v", err)
	}
}

func TestThreadMigrationPreservesExistingPromotions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "013_conversation_threads.sql" {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, "INSERT INTO schema_migrations (version,applied_at) VALUES (?,1)", entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		"INSERT INTO tasks (id,project_id,title,status,priority,created_at,updated_at) VALUES ('tsk_old','prj_default','Old task','backlog','medium',1,1)",
		"INSERT INTO channels (id,project_id,name,created_at) VALUES ('chn_old','prj_default','old',1)",
		"INSERT INTO channel_messages (id,channel_id,project_id,author_type,author_id,body,promoted_task_id,created_at) VALUES ('msg_old','chn_old','prj_default','human','operator','Existing message','tsk_old',2)",
		"INSERT INTO channel_messages (id,channel_id,project_id,author_type,author_id,body,created_at) VALUES ('msg_unlinked','chn_old','prj_default','human','operator','Unrelated',3)",
	} {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	thread, err := s.GetTaskThread(ctx, "tsk_old")
	if err != nil || thread.RootMessageID != "msg_old" {
		t.Fatalf("migrated thread: %#v %v", thread, err)
	}
	history, err := s.PageChannelMessages(ctx, "chn_old", 100, "")
	if err != nil || len(history.Items) != 2 || history.Items[0].ID != "msg_old" || history.Items[0].ThreadID != thread.ID || history.Items[1].ThreadID != "" {
		t.Fatalf("legacy history changed: %#v %v", history, err)
	}
	replay, err := s.PromoteChannelMessage(ctx, "msg_old", domain.CreateTaskInput{})
	if err != nil || replay.ID != "tsk_old" {
		t.Fatalf("legacy promotion replay: %#v %v", replay, err)
	}
}

func TestThreadImportRejectsBrokenLinksAtomicallyAndAcceptsLegacyExports(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "import")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	budget := domain.Budget{TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 1000, MaxCostMinor: 100, Currency: "EUR", MaxConcurrency: 1, Authority: []string{"source.read"}}
	task, err := s.PromoteChannelMessage(ctx, root.ID, domain.CreateTaskInput{Title: "task", Status: domain.TaskBacklog, AcceptanceCriteria: "report", Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.ExportProject(ctx, "prj_default")
	if err != nil {
		t.Fatal(err)
	}
	var bundle portableBundle
	if err = json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*portableBundle)
	}{
		{"foreign project", func(b *portableBundle) { b.Threads[0].ProjectID = "other" }},
		{"missing root membership", func(b *portableBundle) { b.ChannelMessages[0].ThreadID = "" }},
		{"missing thread", func(b *portableBundle) { b.ChannelMessages[0].ThreadID = "missing" }},
		{"lost task origin", func(b *portableBundle) { b.Threads[0].TaskIDs = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var broken portableBundle
			if err := json.Unmarshal(raw, &broken); err != nil {
				t.Fatal(err)
			}
			test.change(&broken)
			data, err := json.Marshal(broken)
			if err != nil {
				t.Fatal(err)
			}
			target, err := Open(ctx, filepath.Join(t.TempDir(), "target.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			if err = target.ImportProject(ctx, data); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("bad import: %v", err)
			}
			if _, err = target.GetTask(ctx, task.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("partial import: %v", err)
			}
			if err = target.ImportProject(ctx, raw); err != nil {
				t.Fatalf("clean retry after rollback: %v", err)
			}
		})
	}
	bundle.Threads = nil
	for i := range bundle.ChannelMessages {
		bundle.ChannelMessages[i].ThreadID = ""
	}
	legacy, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	target, err := Open(ctx, filepath.Join(t.TempDir(), "legacy-import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err = target.ImportProject(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	origin, err := target.GetTaskThread(ctx, task.ID)
	if err != nil || origin.RootMessageID != root.ID {
		t.Fatalf("legacy origin: %#v %v", origin, err)
	}
}

func TestThreadCreationAndTaskLinkRetriesDoNotDuplicateEvents(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "concurrent")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "task", Status: domain.TaskBacklog})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		thread domain.ConversationThread
		err    error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			thread, err := s.CreateThread(ctx, channel.ID, root.ID)
			if err == nil {
				err = s.LinkThreadTask(ctx, thread.ID, task.ID)
			}
			results <- result{thread, err}
		}()
	}
	var threadID string
	for range 8 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if threadID != "" && threadID != result.thread.ID {
			t.Fatal("duplicate thread")
		}
		threadID = result.thread.ID
	}
	events, err := s.ListEvents(ctx, 100, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if counts["conversation.thread.created"] != 1 || counts["conversation.task.linked"] != 1 {
		t.Fatalf("duplicated events: %v", counts)
	}
}

func TestThreadAcceptsConcurrentRepliesWithoutLosingMessages(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "replies.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	channel, err := s.CreateChannel(ctx, "prj_default", "replies")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, AuthorType: "human", AuthorID: "operator", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.CreateThread(ctx, channel.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	for range 8 {
		go func() {
			_, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: channel.ID, ThreadID: thread.ID, AuthorType: "human", AuthorID: "operator", Body: "reply"})
			results <- err
		}()
	}
	for range 8 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	page, err := s.PageThreadMessages(ctx, thread.ID, 100, "")
	if err != nil || len(page.Items) != 9 {
		t.Fatalf("lost replies: %#v %v", page, err)
	}
}
