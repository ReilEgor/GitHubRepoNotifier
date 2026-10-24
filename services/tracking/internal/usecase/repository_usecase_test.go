package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/repository/memory"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/usecase"
)

func TestRepositoryUseCase_GetOrCreate(t *testing.T) {
	tests := []struct {
		name       string
		repoName   string
		wantErr    error
		wantTag    string
		wantStored bool
	}{
		{
			name:       "success - unknown repository is verified and stored",
			repoName:   github.StaticExisting,
			wantTag:    "v1.0.0",
			wantStored: true,
		},
		{name: "error - repository not found", repoName: github.StaticNotFound, wantErr: model.ErrRepositoryNotFound},
		{
			name:     "error - repository has no releases",
			repoName: github.StaticNoReleases,
			wantErr:  model.ErrReleaseNotFound,
		},
		{name: "error - github unavailable", repoName: github.StaticUnavailable, wantErr: model.ErrGitHubUnavailable},
		{name: "error - rate limit", repoName: github.StaticRateLimited, wantErr: model.ErrRateLimitExceeded},
		{name: "error - timeout", repoName: github.StaticTimeout, wantErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repos := memory.NewRepositoryRepository()
			uc := usecase.NewRepositoryUseCase(repos, github.NewStaticClient())

			got, err := uc.GetOrCreate(context.Background(), tt.repoName)

			_, storedErr := repos.GetByName(context.Background(), tt.repoName)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorIs(t, storedErr, model.ErrRepositoryNotFound, "a failed lookup must store nothing")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, got.LastSeenTag)
			assert.NoError(t, storedErr)
		})
	}

	t.Run("success - known repository is returned without asking GitHub", func(t *testing.T) {
		repos := memory.NewRepositoryRepository()
		_, err := repos.Create(context.Background(), github.StaticUnavailable, "v9")
		require.NoError(t, err)
		uc := usecase.NewRepositoryUseCase(repos, github.NewStaticClient())

		got, err := uc.GetOrCreate(context.Background(), github.StaticUnavailable)

		require.NoError(t, err)
		assert.Equal(t, "v9", got.LastSeenTag)
	})
}
