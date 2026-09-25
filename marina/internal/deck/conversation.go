package deck

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

var _ workcontrol.ConversationSource = (*Client)(nil)

func (c *Client) ListConversationInvocations(ctx context.Context, projectID string, afterID int64) ([]workcontrol.ConversationInvocation, error) {
	var response struct {
		Items []workcontrol.ConversationInvocation `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/conversation/invocations?project_id="+url.QueryEscape(projectID)+"&after_id="+strconv.FormatInt(afterID, 10), nil, &response)
	return response.Items, err
}

func (c *Client) PageConversationMessages(ctx context.Context, channelID, cursor string) (workcontrol.ConversationMessagePage, error) {
	var response workcontrol.ConversationMessagePage
	path := "/api/v1/channels/" + url.PathEscape(channelID) + "/messages?limit=100"
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	err := c.do(ctx, http.MethodGet, path, nil, &response)
	return response, err
}

func (c *Client) EnsureConversationQuestion(ctx context.Context, input workcontrol.ConversationQuestionInput) (workcontrol.ConversationQuestion, error) {
	var question workcontrol.ConversationQuestion
	err := c.do(ctx, http.MethodPost, "/api/v1/conversation/questions", input, &question)
	return question, err
}

func (c *Client) PublishConversationOutcome(ctx context.Context, input workcontrol.ConversationOutcomeInput) (workcontrol.ConversationOutcome, error) {
	var outcome workcontrol.ConversationOutcome
	path := "/api/v1/conversation/outcomes"
	if input.Task != nil && input.Task.Status == "ready" {
		path = "/api/v1/conversation/delegations"
	}
	err := c.do(ctx, http.MethodPost, path, input, &outcome)
	return outcome, err
}
