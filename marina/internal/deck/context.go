package deck

import (
	"context"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"net/http"
	"net/url"
)

func (c *Client) GetRunContext(ctx context.Context, runID string) (workcontrol.ResolvedContext, error) {
	var result workcontrol.ResolvedContext
	err := c.do(ctx, http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID)+"/context", nil, &result)
	return result, err
}

func (c *Client) GetRepository(ctx context.Context, project string) (workcontrol.RepositoryRequest, error) {
	var result workcontrol.RepositoryRequest
	err := c.do(ctx, http.MethodGet, "/api/v1/projects/"+url.PathEscape(project)+"/repository", nil, &result)
	return result, err
}
func (c *Client) RecordRepositoryResult(ctx context.Context, project string, result workcontrol.RepositoryResult) error {
	return c.do(ctx, http.MethodPut, "/api/v1/projects/"+url.PathEscape(project)+"/repository/result", result, nil)
}
