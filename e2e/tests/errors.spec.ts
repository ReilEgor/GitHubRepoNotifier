import { test, expect, newEmail, newRepository, trackedRepository, apiKey } from './support';

test.describe('what the user sees when something is wrong', () => {
  test('requests to protected endpoints need the API key', async ({ api }) => {
    const email = newEmail();

    expect((await api.subscribe(email, 'golang/go', null)).status()).toBe(401);
    expect((await api.subscribe(email, 'golang/go', 'wrong')).status()).toBe(401);
    expect((await api.list(email, null)).status()).toBe(401);
    expect((await (await api.list(email)).json()).total, 'a rejected subscribe stores nothing').toBe(0);
  });

  test('the token is the credential of confirm and unsubscribe', async ({ api }) => {
    const unknown = '00000000-0000-4000-8000-000000000000';

    const confirm = await api.confirm(unknown);
    const unsubscribe = await api.unsubscribe(unknown);

    expect(confirm.status()).toBe(404);
    expect(await confirm.json()).toEqual({ error: 'invalid or expired token' });
    expect(unsubscribe.status()).toBe(404);
  });

  const invalidBodies = [
    ['an empty email', { email: '', repository: 'golang/go' }, 'email is required'],
    ['a malformed email', { email: 'not-an-email', repository: 'golang/go' }, ''],
    ['an email with a display name', { email: 'Name <a@b.co>', repository: 'golang/go' }, ''],
    ['a malformed repository', { email: 'a@b.co', repository: 'no-slash' }, ''],
    ['a repository with a path', { email: 'a@b.co', repository: 'a/b/c' }, ''],
  ] as const;
  for (const [name, body, message] of invalidBodies) {
    test(`${name} is rejected with 400`, async ({ request, api }) => {
      const res = await request.post('/api/v1/subscribe', {
        data: body,
        headers: { 'X-Forwarded-For': api.caller, 'X-API-Key': apiKey },
      });

      expect(res.status()).toBe(400);
      expect(JSON.stringify(await res.json())).toContain(message);
    });
  }

  test('a body that is not JSON is rejected with 400', async ({ request, api }) => {
    const res = await request.post('/api/v1/subscribe', {
      data: '{not json',
      headers: { 'Content-Type': 'application/json', 'X-Forwarded-For': api.caller, 'X-API-Key': apiKey },
    });

    expect(res.status()).toBe(400);
  });

  test('listing needs a valid email', async ({ request, api }) => {
    const headers = { 'X-Forwarded-For': api.caller, 'X-API-Key': apiKey };

    expect((await request.get('/api/v1/subscriptions', { headers })).status()).toBe(400);
    expect((await request.get('/api/v1/subscriptions?email=nope', { headers })).status()).toBe(400);
  });

  const upstream = [
    ['a repository that does not exist on GitHub', 'e2e/missing', 404, 'repository not found'],
    ['a repository without releases', 'e2e/empty', 422, 'repository has no releases'],
    ['GitHub being down', 'e2e/down', 503, 'GitHub API is currently unavailable, please try again later'],
    ['the GitHub rate limit being exhausted', 'e2e/limited', 503, 'GitHub API is currently unavailable, please try again later'],
  ] as const;
  for (const [name, repository, status, message] of upstream) {
    test(`${name} gives ${status} and leaves no trace`, async ({ api }) => {
      const email = newEmail();

      const res = await api.subscribe(email, repository);

      expect(res.status()).toBe(status);
      expect(await res.json()).toEqual({ error: message });
      expect((await (await api.list(email)).json()).total).toBe(0);
      expect(await trackedRepository(repository)).toBeUndefined();
    });
  }

  test('a repository that tracking registered is found afterwards', async ({ api }) => {
    const repository = newRepository();
    expect(await trackedRepository(repository)).toBeUndefined();

    expect((await api.subscribe(newEmail(), repository)).status()).toBe(202);

    expect(await trackedRepository(repository)).toBeDefined();
  });
});
