import { test, expect, newEmail, newCaller, rateLimit, apiKey } from './support';

// The stack runs with RATE_LIMIT=5-M for each caller address.
test.describe('rate limit', () => {
  test('a caller over the limit gets 429 on every endpoint while others are served', async ({ request }) => {
    const busy = { 'X-Forwarded-For': newCaller(), 'X-API-Key': apiKey };
    const email = newEmail();
    for (let i = 0; i < rateLimit; i++) {
      const res = await request.get(`/api/v1/subscriptions?email=${email}`, { headers: busy });
      expect(res.status(), `request ${i + 1}`).toBe(200);
    }

    const list = await request.get(`/api/v1/subscriptions?email=${email}`, { headers: busy });
    const confirm = await request.get('/api/v1/confirm/00000000-0000-4000-8000-000000000000', { headers: busy });
    const other = await request.get(`/api/v1/subscriptions?email=${email}`, {
      headers: { 'X-Forwarded-For': newCaller(), 'X-API-Key': apiKey },
    });

    expect(list.status()).toBe(429);
    expect(await list.json()).toEqual({ error: 'limit exceeded' });
    expect(confirm.status()).toBe(429);
    expect(other.status()).toBe(200);
  });

  test('an unauthenticated flood is limited too', async ({ request }) => {
    const headers = { 'X-Forwarded-For': newCaller() };
    const statuses: number[] = [];

    for (let i = 0; i < rateLimit + 2; i++) {
      statuses.push((await request.get('/api/v1/subscriptions?email=a@b.co', { headers })).status());
    }

    expect(statuses.slice(0, rateLimit).every((s) => s === 401)).toBe(true);
    expect(statuses.slice(rateLimit)).toEqual([429, 429]);
  });

  test('health and version are not limited', async ({ request }) => {
    const headers = { 'X-Forwarded-For': newCaller() };

    for (let i = 0; i < rateLimit * 3; i++) {
      expect((await request.get('/health', { headers })).status()).toBe(200);
    }
    expect((await request.get('/version', { headers })).status()).toBe(200);
  });
});
