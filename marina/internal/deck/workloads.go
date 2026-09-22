package deck

import (
	"context"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"net/http"
	"net/url"
)

func (c *Client) PublishWorkloads(ctx context.Context, project string, catalog workcontrol.WorkloadCatalog) error {
	return c.do(ctx, http.MethodPut, "/api/v1/projects/"+url.PathEscape(project)+"/workloads", catalog, nil)
}
