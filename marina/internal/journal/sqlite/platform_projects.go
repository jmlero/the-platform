package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// PlatformProject is a Project resource's repository request and result.
type PlatformProject struct {
	ProjectID, Namespace, Name string
	Request                    workcontrol.RepositoryRequest
	Result                     *workcontrol.RepositoryResult
}

// RequestRepository records a Project resource's repository as a pending
// request. A new revision replaces an older pending one.
func (p *Platform) RequestRepository(ctx context.Context, namespace, name, projectID string, revision int64, input workcontrol.RepositoryRequestInput) error {
	request := workcontrol.RepositoryRequest{RepositoryRequestInput: input, Revision: revision, Status: "pending"}
	encoded, _ := json.Marshal(request)
	_, err := p.store.db.ExecContext(ctx, `INSERT INTO platform_projects (project_id, namespace, name, revision, request_json, status, updated_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?)
		ON CONFLICT(project_id) DO UPDATE SET revision = excluded.revision, request_json = excluded.request_json, status = 'pending', result_json = '', updated_at = excluded.updated_at
		WHERE excluded.revision > platform_projects.revision`, projectID, namespace, name, revision, string(encoded), millis(p.store.now()))
	return err
}

func (p *Platform) Project(ctx context.Context, projectID string) (PlatformProject, error) {
	var out PlatformProject
	var request, result string
	err := p.store.db.QueryRowContext(ctx, "SELECT project_id, namespace, name, request_json, result_json FROM platform_projects WHERE project_id = ?", projectID).
		Scan(&out.ProjectID, &out.Namespace, &out.Name, &request, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return out, notFound("project")
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(request), &out.Request); err != nil {
		return out, err
	}
	if result != "" {
		out.Result = &workcontrol.RepositoryResult{}
		if err := json.Unmarshal([]byte(result), out.Result); err != nil {
			return out, err
		}
	}
	return out, nil
}

// GetRepository returns the project's current repository request; a project
// without a Project resource has nothing pending.
func (p *Platform) GetRepository(ctx context.Context, projectID string) (workcontrol.RepositoryRequest, error) {
	project, err := p.Project(ctx, projectID)
	if isPlatformNotFound(err) {
		return workcontrol.RepositoryRequest{Status: "none"}, nil
	}
	if err != nil {
		return workcontrol.RepositoryRequest{}, err
	}
	request := project.Request
	if project.Result != nil {
		request.Status, request.Reason, request.Binding = project.Result.Status, project.Result.Reason, project.Result.Binding
	}
	return request, nil
}

func (p *Platform) RecordRepositoryResult(ctx context.Context, projectID string, result workcontrol.RepositoryResult) error {
	encoded, _ := json.Marshal(result)
	_, err := p.store.db.ExecContext(ctx, "UPDATE platform_projects SET status = ?, result_json = ?, updated_at = ? WHERE project_id = ? AND revision = ?",
		result.Status, string(encoded), millis(p.store.now()), projectID, result.RequestRevision)
	return err
}

func (p *Platform) PublishWorkloads(ctx context.Context, projectID string, catalog workcontrol.WorkloadCatalog) error {
	encoded, _ := json.Marshal(catalog)
	_, err := p.store.db.ExecContext(ctx, `INSERT INTO platform_workloads (project_id, catalog_json, expires_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET catalog_json = excluded.catalog_json, expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
		projectID, string(encoded), catalog.ExpiresAt, millis(p.store.now()))
	return err
}

// Workloads returns every project's published profiles that have not expired.
func (p *Platform) Workloads(ctx context.Context) (map[string]workcontrol.WorkloadCatalog, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT project_id, catalog_json FROM platform_workloads WHERE expires_at > ?", millis(p.store.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]workcontrol.WorkloadCatalog{}
	for rows.Next() {
		var project, encoded string
		var catalog workcontrol.WorkloadCatalog
		if err := rows.Scan(&project, &encoded); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(encoded), &catalog); err != nil {
			return nil, err
		}
		out[project] = catalog
	}
	return out, rows.Err()
}
