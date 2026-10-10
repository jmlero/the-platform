package github

import (
	"context"
	"errors"
	"net/http"

	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
)

func (client *Client) ContainsCommit(ctx context.Context, repository, ancestor, descendant string) (bool, error) {
	if err := client.repository(repository); err != nil {
		return false, err
	}
	if !validObjectID(ancestor) || !validObjectID(descendant) {
		return false, sourcecontrol.ErrInvalidInput
	}
	if ancestor == descendant {
		return true, nil
	}
	var result struct {
		Status string `json:"status"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	status, err := client.request(ctx, http.MethodGet, "/compare/"+ancestor+"..."+descendant+"?per_page=1", nil, &result, 0)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, httpError("inspect commit ancestry", status)
	}
	if result.Base.SHA != ancestor || !validObjectID(result.MergeBase.SHA) {
		return false, errors.New("Git provider omitted exact comparison ancestry")
	}
	return (result.Status == "ahead" || result.Status == "identical") && result.MergeBase.SHA == ancestor, nil
}
