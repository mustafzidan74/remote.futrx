package project

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidGitHubName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "simple", input: "futrx-com", want: true},
		{name: "with dots", input: "remote.futrx", want: true},
		{name: "with underscores", input: "my_repo", want: true},
		{name: "digits", input: "repo2", want: true},
		{name: "empty", input: ""},
		{name: "leading dash would read as a flag", input: "-repo"},
		{name: "leading dot", input: ".repo"},
		{name: "slash is a separator, not a name", input: "owner/repo"},
		{name: "path traversal", input: ".."},
		{name: "space", input: "my repo"},
		{name: "shell metacharacters", input: "$(whoami)"},
		{name: "too long", input: strings.Repeat("a", 200)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidGitHubName(test.input); got != test.want {
				t.Fatalf("ValidGitHubName(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestGitHubLinkFullName(t *testing.T) {
	tests := []struct {
		name string
		link GitHubLink
		want string
	}{
		{name: "both halves", link: GitHubLink{Owner: "o", Repo: "r"}, want: "o/r"},
		{name: "no owner", link: GitHubLink{Repo: "r"}},
		{name: "no repo", link: GitHubLink{Owner: "o"}},
		{name: "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.link.FullName(); got != test.want {
				t.Fatalf("FullName() = %q, want %q", got, test.want)
			}
		})
	}
}

// githubTestRepo is a project store holding exactly one project.
type githubTestRepo struct {
	meta Meta
}

func (r *githubTestRepo) List(context.Context) ([]Meta, error)            { return []Meta{r.meta}, nil }
func (r *githubTestRepo) Create(_ context.Context, m Meta) (Meta, error)  { return m, nil }
func (r *githubTestRepo) Get(context.Context, ID) (Meta, error)           { return r.meta, nil }
func (r *githubTestRepo) GetBySlug(context.Context, string) (Meta, error) { return r.meta, nil }
func (r *githubTestRepo) Delete(context.Context, ID) error                { return nil }

func (r *githubTestRepo) Update(_ context.Context, _ ID, fn func(*Meta)) (Meta, error) {
	fn(&r.meta)
	return r.meta, nil
}

func (r *githubTestRepo) SetStatus(_ context.Context, _ ID, status Status, msg string) (Meta, error) {
	r.meta.Status = status
	r.meta.ErrorMsg = msg
	return r.meta, nil
}

func githubTestService() (*Service, *githubTestRepo) {
	repo := &githubTestRepo{meta: Meta{ID: "abcd1234", Name: "Demo", Slug: "demo"}}
	return New(repo, ContainerDependencies{}, nil, nil), repo
}

func TestSetGitHubLink(t *testing.T) {
	ctx := context.Background()

	t.Run("stores the link and normalizes the actor", func(t *testing.T) {
		service, repo := githubTestService()
		meta, err := service.SetGitHubLink(ctx, "abcd1234", GitHubLink{
			Owner: "futrx-com", Repo: "remote.futrx", DefaultBranch: "main",
		}, "  Owner@Example.TEST ")
		if err != nil {
			t.Fatalf("SetGitHubLink: %v", err)
		}
		if meta.GitHub == nil || repo.meta.GitHub == nil {
			t.Fatal("the link was not persisted")
		}
		if repo.meta.GitHub.FullName() != "futrx-com/remote.futrx" {
			t.Fatalf("stored repo = %q", repo.meta.GitHub.FullName())
		}
		// Identities are keyed lowercase everywhere else on this platform, and
		// this one authorizes webhook-triggered runs, so it has to match.
		if repo.meta.GitHub.LinkedBy != "owner@example.test" {
			t.Fatalf("LinkedBy = %q, want it trimmed and lowercased", repo.meta.GitHub.LinkedBy)
		}
		if repo.meta.GitHub.LinkedAt == 0 {
			t.Fatal("LinkedAt was not stamped")
		}
	})

	t.Run("rejects a reference that is not two well-formed segments", func(t *testing.T) {
		cases := []GitHubLink{
			{Owner: "", Repo: "r"},
			{Owner: "o", Repo: ""},
			{Owner: "-o", Repo: "r"},
			{Owner: "o", Repo: "../etc"},
			{Owner: "o/x", Repo: "r"},
		}
		for _, link := range cases {
			service, repo := githubTestService()
			_, err := service.SetGitHubLink(ctx, "abcd1234", link, "a@b.test")
			if !errors.Is(err, ErrInvalidGitHubRepo) {
				t.Fatalf("SetGitHubLink(%+v) = %v, want ErrInvalidGitHubRepo", link, err)
			}
			if repo.meta.GitHub != nil {
				t.Fatalf("a rejected link (%+v) must not be persisted", link)
			}
		}
	})

	t.Run("rejects an invalid project id", func(t *testing.T) {
		service, _ := githubTestService()
		_, err := service.SetGitHubLink(ctx, "nope", GitHubLink{Owner: "o", Repo: "r"}, "a@b.test")
		if !errors.Is(err, ErrInvalidID) {
			t.Fatalf("err = %v, want ErrInvalidID", err)
		}
	})
}

func TestClearGitHubLink(t *testing.T) {
	ctx := context.Background()

	t.Run("removes the link", func(t *testing.T) {
		service, repo := githubTestService()
		if _, err := service.SetGitHubLink(ctx, "abcd1234",
			GitHubLink{Owner: "o", Repo: "r"}, "a@b.test"); err != nil {
			t.Fatalf("SetGitHubLink: %v", err)
		}
		meta, err := service.ClearGitHubLink(ctx, "abcd1234")
		if err != nil {
			t.Fatalf("ClearGitHubLink: %v", err)
		}
		if meta.GitHub != nil || repo.meta.GitHub != nil {
			t.Fatal("the link survived an unlink")
		}
	})

	t.Run("is idempotent on a project that was never linked", func(t *testing.T) {
		service, _ := githubTestService()
		if _, err := service.ClearGitHubLink(ctx, "abcd1234"); err != nil {
			t.Fatalf("ClearGitHubLink on an unlinked project: %v", err)
		}
	})

	t.Run("rejects an invalid project id", func(t *testing.T) {
		service, _ := githubTestService()
		if _, err := service.ClearGitHubLink(ctx, "!!"); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("err = %v, want ErrInvalidID", err)
		}
	})
}
