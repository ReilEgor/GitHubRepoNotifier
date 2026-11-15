import { test, expect, newEmail, newRepository, tokenOf, trackedRepository, githubCalls } from './support';

test.describe('subscription journey across subscription, tracking and GitHub', () => {
  test('a subscriber goes from subscribing to unsubscribing', async ({ api }) => {
    const email = newEmail();
    const repository = 'golang/go'; // known to tracking from the seed

    const subscribed = await api.subscribe(email, repository);
    expect(subscribed.status()).toBe(202);

    let listed = await (await api.list(email)).json();
    expect(listed.total).toBe(1);
    expect(listed.subscriptions[0]).toMatchObject({ email, repository_name: repository, confirmed: false });
    const token = await tokenOf(email, repository);
    expect(JSON.stringify(listed), 'the list never shows the token').not.toContain(token);

    expect((await api.confirm(token)).status()).toBe(200);
    listed = await (await api.list(email)).json();
    expect(listed.subscriptions[0].confirmed).toBe(true);

    expect((await api.unsubscribe(token)).status()).toBe(200);
    listed = await (await api.list(email)).json();
    expect(listed.total).toBe(0);
    expect((await api.unsubscribe(token)).status()).toBe(404);
  });

  test('a new repository is verified on GitHub once and remembered by tracking', async ({ api }) => {
    const repository = newRepository();
    expect(await trackedRepository(repository)).toBeUndefined();

    expect((await api.subscribe(newEmail(), repository)).status()).toBe(202);

    expect(await trackedRepository(repository)).toMatchObject({ last_seen_tag: 'v1.0.0' });
    const callsAfterFirst = await githubCalls(repository);
    expect(callsAfterFirst).toBeGreaterThan(0);

    expect((await api.subscribe(newEmail(), repository)).status()).toBe(202);
    expect(await githubCalls(repository), 'a known repository is not asked from GitHub again').toBe(callsAfterFirst);
  });

  test('subscribing to a repository twice keeps one subscription and only the latest link works', async ({ api }) => {
    const email = newEmail();
    const repository = newRepository();

    await api.subscribe(email, repository);
    const first = await tokenOf(email, repository);
    await api.subscribe(email, repository);
    const second = await tokenOf(email, repository);

    expect(second).not.toBe(first);
    expect((await api.confirm(first)).status()).toBe(404);
    expect((await api.confirm(second)).status()).toBe(200);
    expect((await (await api.list(email)).json()).total).toBe(1);
  });

  test('subscribing to a confirmed pair changes nothing', async ({ api }) => {
    const email = newEmail();
    const repository = newRepository();
    await api.subscribe(email, repository);
    const token = await tokenOf(email, repository);
    await api.confirm(token);

    const again = await api.subscribe(email, repository);

    expect(again.status()).toBe(202);
    expect(await tokenOf(email, repository)).toBe(token);
    expect((await (await api.list(email)).json()).subscriptions[0].confirmed).toBe(true);
  });

  test('one person follows several repositories and sees all of them', async ({ api }) => {
    const email = newEmail();
    const repositories = [newRepository(), newRepository(), 'golang/go'];

    for (const repository of repositories) {
      expect((await api.subscribe(email, repository)).status()).toBe(202);
    }

    const listed = await (await api.list(email)).json();
    expect(listed.total).toBe(3);
    const names = listed.subscriptions.map((s: { repository_name: string }) => s.repository_name).sort();
    expect(names).toEqual([...repositories].sort());
  });

  test('parallel subscriptions of one pair end with one subscription', async ({ api }) => {
    const email = newEmail();
    const repository = newRepository();

    const responses = await Promise.all(Array.from({ length: 4 }, () => api.subscribe(email, repository)));

    expect(responses.map((r) => r.status())).toEqual([202, 202, 202, 202]);
    expect((await (await api.list(email)).json()).total).toBe(1);
  });
});
