package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/jmlero/the-platform/deck/internal/domain"
	"net/url"
	"reflect"
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
func (s *Store) RequestRepository(ctx context.Context, project string, input domain.RepositoryRequestInput) (domain.RepositoryRequest, error) {
	if !validRepositoryInput(input) {
		return domain.RepositoryRequest{}, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.RepositoryRequest{}, err
	}
	defer tx.Rollback()
	previous, err := getRepository(ctx, tx, project)
	if err != nil {
		return previous, err
	}
	if previous.Revision == input.ExpectedRevision+1 && reflect.DeepEqual(previous.RepositoryRequestInput, input) {
		return previous, nil
	}
	version := int64(0)
	if previous.Binding != nil {
		version = previous.Binding.Version
	}
	if previous.Revision != input.ExpectedRevision || previous.Status == "pending" || version != input.ExpectedBindingVersion {
		return previous, domain.ErrConflict
	}
	request := domain.RepositoryRequest{RepositoryRequestInput: input, Revision: previous.Revision + 1, Status: "pending", Binding: previous.Binding}
	_, err = tx.ExecContext(ctx, `INSERT INTO repository_requests(project_id,document_json) VALUES (?,?) ON CONFLICT(project_id) DO UPDATE SET document_json=excluded.document_json`, project, marshalJSON(request))
	if err != nil {
		return request, err
	}
	return request, tx.Commit()
}
func (s *Store) RecordRepositoryResult(ctx context.Context, project string, input domain.RepositoryResult) error {
	if input.RequestRevision < 1 || len(input.Reason) > 500 || (input.Status != "connected" && input.Status != "rejected") {
		return domain.ErrInvalidInput
	}
	if input.Status == "connected" && input.Binding == nil {
		return domain.ErrInvalidInput
	}
	if input.Binding != nil {
		if input.Binding.Version < 1 || !validRepositoryInput(domain.RepositoryRequestInput{Repository: input.Binding.Repository, BaseBranch: input.Binding.BaseBranch}) {
			return domain.ErrInvalidInput
		}
		u, err := url.Parse(input.Binding.CloneURL)
		if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || !(u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
			return domain.ErrInvalidInput
		}
	}
	if input.Status == "rejected" && input.Reason == "" {
		return domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	request, err := getRepository(ctx, tx, project)
	if err != nil {
		return err
	}
	if request.Revision != input.RequestRevision {
		return domain.ErrConflict
	}
	if request.Status != "pending" {
		if request.Status == input.Status && request.Reason == input.Reason && (input.Binding == nil || reflect.DeepEqual(request.Binding, input.Binding)) {
			return nil
		}
		return domain.ErrConflict
	}
	if input.Binding != nil {
		if (input.Status == "connected" && (input.Binding.Repository != request.Repository || input.Binding.BaseBranch != request.BaseBranch)) || (request.Binding != nil && input.Binding.Version < request.Binding.Version) {
			return domain.ErrConflict
		}
		request.Binding = input.Binding
	}
	request.Status = input.Status
	request.Reason = input.Reason
	_, err = tx.ExecContext(ctx, "UPDATE repository_requests SET document_json=? WHERE project_id=?", marshalJSON(request), project)
	if err != nil {
		return err
	}
	return tx.Commit()
}
