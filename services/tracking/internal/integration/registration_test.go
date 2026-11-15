//go:build integration

package integration

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/grpc/trackingv1"
)

func (e env) get(name string) (*pb.GetOrCreateRepositoryResponse, error) {
	return e.client.GetOrCreateRepository( //nolint:wrapcheck // the tests assert the gRPC status as it is.
		withKey(apiKey), &pb.GetOrCreateRepositoryRequest{FullName: name})
}

func TestGetOrCreateRepository(t *testing.T) {
	t.Run("success - an unknown repository is verified on GitHub and stored with its latest tag", func(t *testing.T) {
		e := newEnv(t, map[string]githubRepo{"rust-lang/rust": {exists: true, tag: "1.90.0"}}, true)

		resp, err := e.get("rust-lang/rust")

		require.NoError(t, err)
		assert.Equal(t, "rust-lang/rust", resp.GetFullName())
		assert.Equal(t, "1.90.0", resp.GetLastSeenTag())
		assert.NotZero(t, resp.GetId())
		assert.Equal(
			t,
			1,
			e.count(
				t,
				`SELECT count(*) FROM repositories WHERE full_name = 'rust-lang/rust' AND last_seen_tag = '1.90.0'`,
			),
		)
	})

	t.Run("success - a known repository is answered from the database without asking GitHub", func(t *testing.T) {
		e := newEnv(t, nil, true)

		resp, err := e.get("golang/go")

		require.NoError(t, err)
		assert.Equal(t, "go1.25.3", resp.GetLastSeenTag())
		assert.Zero(t, e.github.total())
	})

	t.Run("success - the second request for a new repository does not reach GitHub again", func(t *testing.T) {
		e := newEnv(t, map[string]githubRepo{"rust-lang/rust": {exists: true, tag: "1.90.0"}}, false)
		first, err := e.get("rust-lang/rust")
		require.NoError(t, err)
		callsAfterFirst := e.github.total()

		second, err := e.get("rust-lang/rust")

		require.NoError(t, err)
		assert.Equal(t, first.GetId(), second.GetId())
		assert.Equal(t, callsAfterFirst, e.github.total())
	})

	t.Run("success - parallel requests for one new repository give one row and one id", func(t *testing.T) {
		e := newEnv(t, map[string]githubRepo{"rust-lang/rust": {exists: true, tag: "1.90.0"}}, false)
		const n = 10
		ids := make([]int64, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := e.get("rust-lang/rust")
				if assert.NoError(t, err) {
					ids[i] = resp.GetId()
				}
			}()
		}
		wg.Wait()

		for _, id := range ids {
			assert.Equal(t, ids[0], id)
		}
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM repositories`))
	})
}

func TestGetOrCreateRepository_failures(t *testing.T) {
	tests := []struct {
		name     string
		repos    map[string]githubRepo
		repoName string
		want     codes.Code
	}{
		{"error - the repository does not exist on GitHub", nil, "no/such", codes.NotFound},
		{
			"error - the repository has no release",
			map[string]githubRepo{"o/empty": {exists: true}},
			"o/empty",
			codes.FailedPrecondition,
		},
		{"error - GitHub is down", map[string]githubRepo{"o/down": {failStatus: 503}}, "o/down", codes.Unavailable},
		{
			"error - GitHub rate limit is exhausted",
			map[string]githubRepo{"o/limited": {failStatus: 403, remaining: "0"}},
			"o/limited",
			codes.ResourceExhausted,
		},
		{
			"error - GitHub refuses with a plain 403",
			map[string]githubRepo{"o/denied": {failStatus: 403}},
			"o/denied",
			codes.Internal,
		},
		{"error - the name is malformed", nil, "not-a-repo", codes.InvalidArgument},
		{"error - the name has a path traversal", nil, "../../etc/passwd", codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name+" and nothing is stored", func(t *testing.T) {
			e := newEnv(t, tt.repos, false)

			_, err := e.get(tt.repoName)

			require.Error(t, err)
			assert.Equal(t, tt.want, status.Code(err), err.Error())
			assert.Zero(t, e.count(t, `SELECT count(*) FROM repositories`))
		})
	}

	t.Run("error - GitHub is retried before the repository is declared unavailable", func(t *testing.T) {
		e := newEnv(t, map[string]githubRepo{"o/down": {failStatus: 503}}, false)

		_, err := e.get("o/down")

		require.Error(t, err)
		assert.Equal(t, 2, e.github.total(), "two attempts are configured")
	})

	t.Run("error - a malformed name never reaches GitHub", func(t *testing.T) {
		e := newEnv(t, nil, false)

		_, err := e.get("a b/c")

		require.Error(t, err)
		assert.Zero(t, e.github.total())
	})
}

func TestGetOrCreateRepository_authentication(t *testing.T) {
	for name, key := range map[string]string{"error - no key": "", "error - wrong key": "nope"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, map[string]githubRepo{"rust-lang/rust": {exists: true, tag: "1.90.0"}}, false)

			_, err := e.client.GetOrCreateRepository(
				withKey(key),
				&pb.GetOrCreateRepositoryRequest{FullName: "rust-lang/rust"},
			)

			assert.Equal(t, codes.Unauthenticated, status.Code(err))
			assert.Zero(t, e.github.total(), "an unauthenticated call does no work")
			assert.Zero(t, e.count(t, `SELECT count(*) FROM repositories`))
		})
	}
}

func TestGetOrCreateRepository_circuitBreaker(t *testing.T) {
	t.Run("error - after repeated GitHub failures calls are rejected without reaching GitHub", func(t *testing.T) {
		e := newEnv(t, map[string]githubRepo{"o/down": {failStatus: 503}}, false)
		for range 3 { // the breaker trips after three failed series.
			_, err := e.get("o/down")
			require.Equal(t, codes.Unavailable, status.Code(err))
		}
		before := e.github.total()

		_, err := e.get("o/down")

		assert.Equal(t, codes.Unavailable, status.Code(err))
		assert.Equal(t, before, e.github.total(), "the open breaker protects GitHub and the caller's latency")
	})
}
