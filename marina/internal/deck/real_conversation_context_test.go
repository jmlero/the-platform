package deck

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func TestRealConversationContext(t *testing.T) {
	serverURL, tokenFile := os.Getenv("DECK_CONFORMANCE_URL"), os.Getenv("DECK_CONFORMANCE_TOKEN_FILE")
	if serverURL == "" && tokenFile == "" {
		t.Skip("set real Deck conformance URL and token file")
	}
	client, err := New(serverURL, &http.Client{Timeout: 3 * time.Second}, WithBearerTokenFile(tokenFile))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := waitForProvider(ctx, client); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CheckCompatibility(ctx, []string{"conversation.context.read"}); err != nil {
		t.Fatal(err)
	}
	agentID := "agt_context_conformance"
	if _, err := client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: agentID, InstallationID: "context-conformance", Name: "Context conformance", Kind: "conversation", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	var channel struct {
		ID string `json:"id"`
	}
	if err := client.do(ctx, http.MethodPost, "/api/v1/operator/channels", map[string]string{"project_id": "prj_default", "name": "context conformance"}, &channel); err != nil {
		t.Fatal(err)
	}
	input := workcontrol.ContextSourceInput{ID: "chat-context-proof", Kind: "instruction", Title: "Context proof", Content: "Preserve the API", Trust: "guidance", State: "active"}
	var source workcontrol.ContextSource
	if err := client.do(ctx, http.MethodPut, "/api/v1/operator/projects/prj_default/context/sources", input, &source); err != nil {
		t.Fatal(err)
	}
	invocationInput := map[string]string{"agent_id": agentID, "request_id": "chat-context-conformance-1", "body": "Explain the selected project instructions"}
	var invocation struct {
		Message workcontrol.ConversationMessage `json:"message"`
	}
	endpoint := "/api/v1/operator/channels/" + channel.ID + "/invocations"
	if err := client.do(ctx, http.MethodPost, endpoint, invocationInput, &invocation); err != nil {
		t.Fatal(err)
	}
	frozen, err := client.GetConversationContext(ctx, invocation.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workcontrol.ValidateResolvedContext(frozen.Selection, frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Selection == nil || len(frozen.Sources) != 1 || frozen.Sources[0].Digest != source.Digest {
		t.Fatalf("frozen context: %#v", frozen)
	}
	input.ExpectedRevision, input.Content = source.Revision, "New guidance only affects new invocations"
	var revised workcontrol.ContextSource
	if err := client.do(ctx, http.MethodPut, "/api/v1/operator/projects/prj_default/context/sources", input, &revised); err != nil {
		t.Fatal(err)
	}
	if err := client.do(ctx, http.MethodPost, endpoint, invocationInput, &invocation); err != nil {
		t.Fatal(err)
	}
	replayed, err := client.GetConversationContext(ctx, invocation.Message.ID)
	if err != nil || !reflect.DeepEqual(frozen, replayed) {
		t.Fatalf("replay silently changed context: %#v %v", replayed, err)
	}
	if err := client.do(ctx, http.MethodPost, "/api/v1/operator/projects/prj_default/context/sources/"+source.ID+"/1/revoke", nil, nil); err != nil {
		t.Fatal(err)
	}
	revoked, err := client.GetConversationContext(ctx, invocation.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workcontrol.ValidateResolvedContext(frozen.Selection, revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.Sources[0].RevokedAt == 0 || workcontrol.CheckContextValidity(time.Now(), revoked) == nil {
		t.Fatal("revocation not enforced by consumer")
	}
	_, err = client.GetConversationContext(ctx, "msg_missing_context_conformance")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("missing context: %v", err)
	}
	// Do not leave the conformance-only guidance active for later workflow tests.
	if err := client.do(ctx, http.MethodPost, "/api/v1/operator/projects/prj_default/context/sources/"+source.ID+"/2/revoke", nil, nil); err != nil {
		t.Fatal(err)
	}
}
