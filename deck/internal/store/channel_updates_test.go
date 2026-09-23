package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestChannelMessagesPublishDurableEventsAndPageWithoutGaps(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "channels.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ch, err := s.CreateChannel(ctx, "prj_default", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateChannel(ctx, "prj_default", "other")
	if err != nil {
		t.Fatal(err)
	}
	live, unsubscribe := s.Subscribe(300)
	defer unsubscribe()
	var ids []string
	for i := 0; i < 245; i++ {
		msg, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, AuthorType: "human", AuthorID: "operator:test", Body: fmt.Sprint(i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, msg.ID)
	}
	// A timestamp tie (or clock correction) must not reorder an append cursor.
	if _, err = s.db.ExecContext(ctx, "UPDATE channel_messages SET created_at = 123"); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListEvents(ctx, 500, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 245 || events[0].Type != "channel.message.created" {
		t.Fatalf("message events missing: %d", len(events))
	}
	payload := events[0].Payload
	if payload["channel_id"] != ch.ID || payload["message_id"] != ids[244] {
		t.Fatalf("event correlation: %v", payload)
	}
	select {
	case event := <-live:
		if event.Type != "channel.message.created" {
			t.Fatalf("live event: %s", event.Type)
		}
	default:
		t.Fatal("message was not published live")
	}
	cursor := ""
	var got []string
	for {
		page, err := s.PageChannelMessages(ctx, ch.ID, 100, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range page.Items {
			got = append(got, msg.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if len(got) == 100 {
			message, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: ch.ID, AuthorType: "human", AuthorID: "operator:test", Body: "concurrent append"})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, message.ID)
			if _, err = s.db.ExecContext(ctx, "UPDATE channel_messages SET created_at = 123 WHERE id = ?", message.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(ids) {
		t.Fatalf("history skipped/reordered/duplicated messages: %d", len(got))
	}
	if _, err = s.PageChannelMessages(ctx, other.ID, 100, cursor); err == nil {
		t.Fatal("cross-channel cursor accepted")
	}
	if _, err = s.PageChannelMessages(ctx, ch.ID, 100, "bad cursor"); err == nil {
		t.Fatal("malformed cursor accepted")
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
	if err = restored.ImportProject(ctx, exported); err != nil {
		t.Fatal(err)
	}
	restoredIDs := []string{}
	restoredCursor := ""
	for {
		page, err := restored.PageChannelMessages(ctx, ch.ID, 100, restoredCursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range page.Items {
			restoredIDs = append(restoredIDs, message.ID)
		}
		if page.NextCursor == "" {
			break
		}
		restoredCursor = page.NextCursor
	}
	if fmt.Sprint(restoredIDs) != fmt.Sprint(ids) {
		t.Fatal("export/import reordered channel history")
	}

}
