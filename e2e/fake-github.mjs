// A stand-in for api.github.com for the end-to-end tests: it answers the two calls tracking makes and counts them.
// The behaviour is chosen by the repository name, so every test can ask for the outcome it needs:
//   fresh/<anything>   exists, latest release v1.0.0
//   e2e/empty          exists, no release
//   e2e/down           always 503
//   e2e/limited        403 with X-RateLimit-Remaining: 0 (the GitHub rate limit)
//   anything else      404
// GET /__calls?repo=<name> returns how many requests the repository received.
import http from 'node:http';

const port = Number(process.env.E2E_GITHUB_PORT ?? 18099);
const calls = new Map();

function outcome(name) {
  if (name.startsWith('fresh/')) return { exists: true, tag: 'v1.0.0' };
  switch (name) {
    case 'e2e/empty':
      return { exists: true };
    case 'e2e/down':
      return { status: 503 };
    case 'e2e/limited':
      return { status: 403, remaining: '0' };
    default:
      return { exists: false };
  }
}

http
  .createServer((req, res) => {
    const url = new URL(req.url, 'http://localhost');
    if (url.pathname === '/__calls') {
      const repo = url.searchParams.get('repo') ?? '';
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ calls: calls.get(repo) ?? 0 }));
      return;
    }
    const match = url.pathname.match(/^\/repos\/([^/]+\/[^/]+?)(\/releases\/latest)?$/);
    if (!match) {
      res.statusCode = 404;
      res.end();
      return;
    }
    const [, name, release] = match;
    calls.set(name, (calls.get(name) ?? 0) + 1);

    const o = outcome(name);
    if (o.status) {
      if (o.remaining) res.setHeader('X-RateLimit-Remaining', o.remaining);
      res.statusCode = o.status;
      res.end();
    } else if (!o.exists || (release && !o.tag)) {
      res.statusCode = 404;
      res.end();
    } else if (release) {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ tag_name: o.tag }));
    } else {
      res.statusCode = 200;
      res.end();
    }
  })
  .listen(port, '127.0.0.1', () => console.log(`fake github on :${port}`));
