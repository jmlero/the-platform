package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

type ProjectChannel struct {
	Project    domain.Project           `json:"project"`
	Channels   []domain.Channel         `json:"channels"`
	Repository domain.RepositoryRequest `json:"repository"`
}

func (s *Store) ProjectChannels(ctx context.Context) ([]ProjectChannel, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, slug, description, created_at FROM projects ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	result := []ProjectChannel{}
	for rows.Next() {
		var item ProjectChannel
		if err := rows.Scan(&item.Project.ID, &item.Project.Name, &item.Project.Slug, &item.Project.Description, &item.Project.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Channels, err = s.ListChannels(ctx, result[i].Project.ID)
		if err != nil {
			return nil, err
		}
		result[i].Repository, err = s.GetRepository(ctx, result[i].Project.ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// New projects have one main channel and one repository association request.
// Credentials and actual repository validation remain Marina-owned.
func (s *Store) CreateProjectChannel(ctx context.Context, repository, branch string) (ProjectChannel, error) {
	repository = strings.TrimSpace(repository)
	branch = strings.TrimSpace(branch)
	input := domain.RepositoryRequestInput{Repository: repository, BaseBranch: branch}
	if !validRepositoryInput(input) {
		return ProjectChannel{}, fmt.Errorf("use owner/repository and a valid base branch: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectChannel{}, err
	}
	defer tx.Rollback()
	// Stable repository slug also serializes duplicate creation after lost responses.
	slug := strings.ToLower(strings.ReplaceAll(repository, "/", "--"))
	id := newID("prj")
	now := nowMillis()
	_, err = tx.ExecContext(ctx, "INSERT INTO projects (id,name,slug,description,created_at) VALUES (?,?,?,'',?) ON CONFLICT(slug) DO NOTHING", id, repository, slug, now)
	if err != nil {
		return ProjectChannel{}, err
	}
	var item ProjectChannel
	err = tx.QueryRowContext(ctx, "SELECT id,name,slug,description,created_at FROM projects WHERE slug = ?", slug).Scan(&item.Project.ID, &item.Project.Name, &item.Project.Slug, &item.Project.Description, &item.Project.CreatedAt)
	if err != nil {
		return item, err
	}
	previous, err := getRepository(ctx, tx, item.Project.ID)
	if err != nil {
		return item, err
	}
	if previous.Revision > 0 {
		if previous.Repository != repository || previous.BaseBranch != branch {
			return item, domain.ErrConflict
		}
	} else {
		previous = domain.RepositoryRequest{RepositoryRequestInput: input, Revision: 1, Status: "pending"}
		if _, err = tx.ExecContext(ctx, "INSERT INTO repository_requests(project_id,document_json) VALUES (?,?)", item.Project.ID, marshalJSON(previous)); err != nil {
			return item, err
		}
	}
	item.Repository = previous
	_, err = tx.ExecContext(ctx, "INSERT INTO channels (id,project_id,name,created_at) VALUES (?,?,?,?) ON CONFLICT(project_id,name) DO NOTHING", newID("chn"), item.Project.ID, strings.Split(repository, "/")[1], now)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	item.Channels, err = s.ListChannels(ctx, item.Project.ID)
	return item, err
}
