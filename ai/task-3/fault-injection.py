"""Fault injection: apply one realistic bug at a time and see which test level catches it.

Usage (from anywhere, needs Docker and Node.js): python ai/task-3/fault-injection.py [B01 B02 ...]
Overwrites fault-injection-results.json with the bugs that were run.

Cascade per bug: unit -> integration -> e2e; stops at the first level that fails.
The file is always restored with `git checkout`.
"""
import json
import os
import re
import subprocess
import sys

from pathlib import Path

ROOT = str(Path(__file__).resolve().parents[2])
OUT = str(Path(__file__).with_name("fault-injection-results.json"))
os.environ["TESTCONTAINERS_RYUK_DISABLED"] = "true"

SUB = "services/subscription"
TRK = "services/tracking"
UC = SUB + "/internal/usecase/subscription_usecase.go"
REPO = SUB + "/internal/repository/postgres/subscription_repository.go"

BUGS = [
    dict(id="B01", mod=SUB, file=UC, desc="re-subscribing a confirmed pair queues a new confirmation email and saga",
         old="\tif sub.Confirmed {\n\t\treturn nil\n\t}\n", new=""),
    dict(id="B02", mod=SUB, file=UC, desc="confirm publishes the activation event to the wrong queue",
         old="contracts.QueueSubscriptionActivated, contracts.SubscriptionActivatedEvent{",
         new="contracts.QueueUnsubscriptionActivated, contracts.SubscriptionActivatedEvent{"),
    dict(id="B03", mod=SUB, file=UC, desc="unsubscribe swallows the error of Delete (double unsubscribe succeeds)",
         old='\t\t\treturn fmt.Errorf("delete subscription: %w", deleteErr)\n',
         new='\t\t\tslog.Debug("ignored", slog.String("error", deleteErr.Error()))\n'),
    dict(id="B04", mod=SUB, file=REPO, desc="a confirmed subscription gets a new token on re-subscribe",
         old="SET token = CASE WHEN subscriptions.is_confirmed THEN subscriptions.token ELSE EXCLUDED.token END",
         new="SET token = EXCLUDED.token"),
    dict(id="B05", mod=SUB, file=REPO, desc="Confirm does not notice that no row was changed",
         old="\tif tag.RowsAffected() == 0 {\n\t\treturn model.ErrInvalidToken\n\t}\n", new="\t_ = tag.RowsAffected()\n", which=0),
    dict(id="B06", mod=SUB, file=REPO, desc="Delete does not notice that no row was deleted",
         old="\tif tag.RowsAffected() == 0 {\n\t\treturn model.ErrInvalidToken\n\t}\n", new="\t_ = tag.RowsAffected()\n", which=1),
    dict(id="B07", mod=SUB, file=REPO, desc="the list is oldest first instead of newest first",
         old="ORDER BY s.created_at DESC, s.id DESC", new="ORDER BY s.created_at ASC, s.id ASC"),
    dict(id="B08", mod=SUB, file=SUB + "/internal/transport/http/middleware/request.go",
         desc="API key check accepts any header that starts with the key",
         old="subtle.ConstantTimeCompare([]byte(c.GetHeader(HeaderAPIKey)), []byte(key)) != 1",
         new="!(len(c.GetHeader(HeaderAPIKey)) >= len(key) && subtle.ConstantTimeCompare([]byte(c.GetHeader(HeaderAPIKey)[:len(key)]), []byte(key)) == 1)"),
    dict(id="B09", mod=SUB, file=SUB + "/internal/transport/http/middleware/ratelimit.go",
         desc="rate limiter fails closed instead of open when Redis is down",
         old='slog.String("error", err.Error()))\n\t\t\tc.Next()\n\t\t\treturn',
         new='slog.String("error", err.Error()))\n\t\t\tFail(c, ErrRateLimited)\n\t\t\tc.Abort()\n\t\t\treturn'),
    dict(id="B10", mod=SUB, file=SUB + "/internal/infrastructure/adapter/tracking_client.go",
         desc="the GitHub rate limit of tracking is no longer translated to 503",
         old="\tcodes.ResourceExhausted:  model.ErrServiceUnavailable,\n", new=""),
    dict(id="B11", mod=TRK, file=TRK + "/internal/transport/grpc/server.go",
         desc="GitHub rate limit is reported as Unavailable instead of ResourceExhausted",
         old='status.Error(codes.ResourceExhausted, "github rate limit exceeded")',
         new='status.Error(codes.Unavailable, "github rate limit exceeded")'),
    dict(id="B12", mod=TRK, file=TRK + "/internal/usecase/repository_usecase.go",
         desc="a new repository is stored without its latest tag",
         old="u.repos.Create(ctx, fullName, release.TagName)", new='u.repos.Create(ctx, fullName, release.TagName[:0])'),
    dict(id="B13", mod=TRK, file=TRK + "/internal/usecase/repository_usecase.go",
         desc="existence of the repository is not checked before reading its release",
         old='\tif checkErr := u.github.CheckRepository(ctx, fullName); checkErr != nil {\n\t\treturn model.Repository{}, fmt.Errorf("%s: check repository: %w", op, checkErr)\n\t}\n',
         new=""),
    dict(id="B14", mod=TRK, file=TRK + "/internal/repository/postgres/repository_repository.go",
         desc="parallel registration of one repository fails (DO NOTHING returns no row)",
         old="ON CONFLICT (full_name) DO UPDATE SET full_name = EXCLUDED.full_name", new="ON CONFLICT (full_name) DO NOTHING"),
    dict(id="B15", mod=SUB, file=SUB + "/internal/infrastructure/ratelimit/ratelimit.go",
         desc="the Redis client ignores the context deadline again",
         old="ContextTimeoutEnabled: true, ", new=""),

    dict(id="B16", mod=SUB, file=SUB + "/internal/transport/http/handler.go",
         desc="the 254-character limit of an email address is not enforced",
         old="\tif len(email) > maxEmailLen {\n\t\treturn false\n\t}\n", new=""),
    dict(id="B17", mod=SUB, file=SUB + "/internal/transport/http/handler.go",
         desc="a repository name part may be 10000 characters long",
         old="{1,100}/[A-Za-z0-9._-]{1,100}$", new="{1,100}/[A-Za-z0-9._-]{1,10000}$"),
    dict(id="B18", mod=SUB, file=UC,
         desc="the confirmation command loses the request id (broken log trace)",
         old="\t\tSubscriptionID: sub.ID,\n\t\tRequestID:      ctxlog.RequestID(txCtx),\n",
         new="\t\tSubscriptionID: sub.ID,\n"),
]

INTEGRATION = {
    SUB: ["./internal/integration/"],
    TRK: ["./internal/integration/", "./internal/repository/postgres/"],
}


def sh(cmd, cwd, timeout=900):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout, encoding="utf-8", errors="replace")
    return p.returncode, p.stdout + p.stderr


def failing(out):
    names = re.findall(r"^\s*--- FAIL: (\S+)", out, re.M)
    if not names:
        names = re.findall(r"✘\s+\d+\s+(.+?)\s+\(", out)
    return names[:6]


def apply(bug):
    path = os.path.join(ROOT, bug["file"])
    text = open(path, encoding="utf-8", newline="").read()
    old = bug["old"]
    which = bug.get("which")
    if text.count(old) == 0:
        return False
    if which is None:
        if text.count(old) != 1:
            return False
        text = text.replace(old, bug["new"])
    else:
        idx = -1
        for _ in range(which + 1):
            idx = text.index(old, idx + 1)
        text = text[:idx] + bug["new"] + text[idx + len(old):]
    open(path, "w", encoding="utf-8", newline="").write(text)
    return True


def run(bug):
    res = dict(id=bug["id"], desc=bug["desc"], file=bug["file"])
    if not apply(bug):
        res["verdict"] = "patch did not apply"
        return res
    try:
        mod = os.path.join(ROOT, bug["mod"])
        rc, out = sh(["go", "build", "./..."], mod)
        if rc != 0:
            res["verdict"] = "does not compile"
            res["detail"] = out[-400:]
            return res
        rc, out = sh(["go", "test", "-count=1", "./..."], mod)
        res["unit"] = "FAIL" if rc else "pass"
        if rc:
            res["caught_by"] = "unit"
            res["tests"] = failing(out)
            return res
        rc, out = sh(["go", "test", "-count=1", "-tags", "integration"] + INTEGRATION[bug["mod"]], mod)
        res["integration"] = "FAIL" if rc else "pass"
        if rc:
            res["caught_by"] = "integration"
            res["tests"] = failing(out)
            return res
        rc, out = sh(["sh", "scripts/check.sh", "e2e"], ROOT)
        res["e2e"] = "FAIL" if rc else "pass"
        if rc:
            res["caught_by"] = "e2e"
            res["tests"] = failing(out)
            return res
        res["caught_by"] = "NOTHING"
        return res
    finally:
        subprocess.run(["git", "checkout", "--", bug["file"]], cwd=ROOT)


def main():
    only = sys.argv[1:]
    results = []
    for bug in BUGS:
        if only and bug["id"] not in only:
            continue
        r = run(bug)
        results.append(r)
        print(json.dumps(r, ensure_ascii=False), flush=True)
        json.dump(results, open(OUT, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print("DONE", flush=True)


main()
