import { test as base, expect, APIRequestContext, APIResponse } from '@playwright/test';
import { randomUUID, randomInt } from 'node:crypto';
import pg from 'pg';

export const apiKey = process.env.E2E_API_KEY ?? 'smoke-key';
const githubURL = process.env.E2E_GITHUB_URL ?? 'http://127.0.0.1:18099';
export const rateLimit = Number(process.env.E2E_RATE_LIMIT ?? 5);

const subscriptionDB = new pg.Pool({ connectionString: process.env.E2E_SUBSCRIPTION_DB, max: 2 });
const trackingDB = new pg.Pool({ connectionString: process.env.E2E_TRACKING_DB, max: 2 });

/** A caller address nobody used before: the stack trusts X-Forwarded-For from localhost, so every test has its own rate limit bucket. */
export function newCaller(): string {
  return `10.${randomInt(0, 256)}.${randomInt(0, 256)}.${randomInt(1, 255)}`;
}

export function newEmail(): string {
  return `e2e-${randomUUID()}@example.com`;
}

/** A repository the fake GitHub knows, with the latest release v1.0.0; every call gets its own, so tests never share state. */
export function newRepository(): string {
  return `fresh/${randomUUID().slice(0, 8)}`;
}

export class Api {
  /** Every request comes from a new address, so a long journey never meets the rate limit; the rate limit tests use their own. */
  get caller(): string {
    return newCaller();
  }

  constructor(private readonly request: APIRequestContext) {}

  private headers(extra: Record<string, string> = {}): Record<string, string> {
    return { 'X-Forwarded-For': this.caller, ...extra };
  }

  subscribe(email: string, repository: string, key: string | null = apiKey): Promise<APIResponse> {
    return this.request.post('/api/v1/subscribe', {
      data: { email, repository },
      headers: this.headers(key === null ? {} : { 'X-API-Key': key }),
    });
  }

  list(email: string, key: string | null = apiKey): Promise<APIResponse> {
    return this.request.get(`/api/v1/subscriptions?email=${encodeURIComponent(email)}`, {
      headers: this.headers(key === null ? {} : { 'X-API-Key': key }),
    });
  }

  confirm(token: string): Promise<APIResponse> {
    return this.request.get(`/api/v1/confirm/${token}`, { headers: this.headers() });
  }

  unsubscribe(token: string): Promise<APIResponse> {
    return this.request.get(`/api/v1/unsubscribe/${token}`, { headers: this.headers() });
  }
}

/** The token goes to the user by email, which the system does not send yet, so the test reads it from the database. */
export async function tokenOf(email: string, repository: string): Promise<string> {
  const { rows } = await subscriptionDB.query(
    `SELECT s.token FROM subscriptions s
       JOIN users u ON u.id = s.user_id
       JOIN repositories r ON r.id = s.repository_id
      WHERE u.email = $1 AND r.full_name = $2`,
    [email, repository],
  );
  expect(rows, `a subscription of ${email} to ${repository}`).toHaveLength(1);
  return rows[0].token;
}

export async function trackedRepository(name: string): Promise<{ id: string; last_seen_tag: string } | undefined> {
  const { rows } = await trackingDB.query('SELECT id, last_seen_tag FROM repositories WHERE full_name = $1', [name]);
  return rows[0];
}

/** How many requests the fake GitHub received for the repository. */
export async function githubCalls(repository: string): Promise<number> {
  const res = await fetch(`${githubURL}/__calls?repo=${encodeURIComponent(repository)}`);
  return (await res.json()).calls;
}

export const test = base.extend<{ api: Api }>({
  api: async ({ request }, use) => {
    await use(new Api(request));
  },
});

export { expect };
