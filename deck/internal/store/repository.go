package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/jmlero/the-platform/deck/internal/domain"
	"regexp"
	"strings"
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}$`)

func validRepositoryInput(i domain.RepositoryRequestInput) bool {
	return repositoryPattern.MatchString(i.Repository) && i.ExpectedRevision >= 0 && i.ExpectedBindingVersion >= 0 && i.BaseBranch != "" && len(i.BaseBranch) <= 200 && !strings.ContainsAny(i.BaseBranch, " ~^:?*[\\\x00\r\n") && !strings.Contains(i.BaseBranch, "..") && !strings.HasPrefix(i.BaseBranch, "/") && !strings.HasSuffix(i.BaseBranch, "/")
}
func getRepository(ctx context.Context, q contextQuerier, project string) (domain.RepositoryRequest, error) {
	var encoded string
	var result domain.RepositoryRequest
	err := q.QueryRowContext(ctx, "SELECT document_json FROM repository_requests WHERE project_id = ?", project).Scan(&encoded)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(encoded), &result)
	return result, err
}
func (s *Store) GetRepository(ctx context.Context, project string) (domain.RepositoryRequest, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM projects WHERE id=?", project).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			err = domain.ErrNotFound
		}
		return domain.RepositoryRequest{}, err
	}
	return getRepository(ctx, s.db, project)
}
