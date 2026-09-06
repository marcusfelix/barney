# Barney

**Put an AI developer on your team with one file.**

Barney is a self-hosted service that turns GitHub events into agent work. Drop a
`.barney/manifest.yaml` into your repo, and every issue, comment, or push can dispatch an AI
agent (like [opencode](https://opencode.ai)) that works in your codebase and opens a pull
request with the result.

No workflow files, no CI scripts, no runner minutes. You describe *when* the agent should act
and *what* to ask it — Barney handles the rest: secure webhook intake, workspace cloning,
branching, agent execution, commits, pushes, and PR creation.

## Why Barney

- **One file per repo** — `.barney/manifest.yaml` lives in your repo, versioned with your code.
  Change triggers or prompts like any other config.
- **Your infra, your keys** — self-hosted. Your token and agent credentials never leave your
  environment.
- **Any agent** — pluggable harnesses; ships with opencode out of the box.
- **Event-driven** — reacts to issues, issue comments, pull requests, PR review comments, and
  pushes.

## Quick start

### 1. Run Barney

```sh
cp .env.example .env    # add your webhook secret, GitHub token, and agent API key
docker compose up --build
```

### 2. Point GitHub at it

Add a webhook on your repo (Settings → Webhooks) pointing to `http://<your-host>:8080/webhook`
with your `WEBHOOK_SECRET`. For local development:

```sh
gh webhook forward --repo=your-org/your-repo --events=issues,issue_comment,pull_request,push \
  --url=http://localhost:8080/webhook
```

### 3. Add the manifest

Commit this to your repo:

```yaml
# .barney/manifest.yaml
version: "v0"
triggers:
  - id: label-task
    event: issues.opened
    filter: payload.issue.labels.exists(l, l.name == 'agent-task')
    agent: opencode
    prompt_template: |
      Issue: {{ .payload.issue.title }}

      {{ .payload.issue.body }}

      Implement the change described above, commit it, and make sure tests pass.
```

Now anyone with triage permission can label an issue `agent-task` and Barney will clone the
repo, run the agent on it, and open a PR — typically within a couple of minutes.

## Recipes

**Slash-command on issues** — comment `/barney fix the flaky login test`:

```yaml
  - id: slash-command
    event: issue_comment.created
    filter: payload.comment.body.startsWith('/barney')
    agent: opencode
    prompt_template: "The user wrote: {{ .payload.comment.body }}\nDo what they asked on issue #{{ .payload.issue.number }}."
```

**PR review helper** — summarize every opened PR:

```yaml
  - id: pr-summary
    event: pull_request.opened
    filter: '!payload.pull_request.draft'
    agent: opencode
    prompt_template: |
      Review PR #{{ .payload.pull_request.number }}: {{ .payload.pull_request.title }}
      Post a concise risk assessment of the diff as a comment on the PR.
```

**Respond to pushes on main**:

```yaml
  - id: post-merge
    event: push
    filter: payload.ref == 'refs/heads/main'
    agent: opencode
    prompt_template: "A push landed on main. Run the test suite and open a fix PR if anything fails."
```

## How it works

Each triggering event gets its own branch (`barney/<event>-<delivery>`). The agent works
there; if it produces changes, Barney commits them as `barney: automated update for <event>
#<issue>` and opens a PR against your default branch. For `pull_request` events, the agent
works on the PR's own code. Events on the same repo are processed one at a time.

Supported events: `issues`, `issue_comment`, `pull_request`,
`pull_request_review_comment`, `push`.

### Choosing when triggers fire

A trigger fires when the event matches `event: "<type>.<action>"` *and* the optional CEL
`filter` passes. `filter` is an expression evaluated against the raw GitHub payload, so you
can gate on anything GitHub sends: labels, authors, titles, draft state, branches. Omit it and
the trigger fires on every matching event. See the examples above; malformed filters are
skipped, not fatal.

## Configuration

| Environment      | Default                      | Required | Notes                                  |
| ---------------- | ---------------------------- | -------- | -------------------------------------- |
| `WEBHOOK_SECRET` | —                            | yes      | HMAC secret for webhook validation     |
| `GITHUB_TOKEN`   | —                            | yes      | Used for clone/fetch/push and `gh`     |
| `PORT`           | `8080`                       | no       |                                        |
| `WORKSPACE_ROOT` | `/var/lib/barney/workspaces` | no       | Where repos are cloned                 |
| `EVENT_TIMEOUT`  | `30m`                        | no       | Per-event processing limit             |
| `COMMITTER_NAME` | `barney`                     | no       | Git author name on automated commits   |
| `COMMITTER_EMAIL`| `barney@users.noreply.github.com` | no  | Git author email on automated commits  |

### Agent credentials (stateless)

There is no `opencode auth login` step — Barney is stateless by design. Provider keys are
injected as environment variables (`.env` / `env_file` / `docker run --env-file`) and
inherited by the opencode process on every run. Uncomment the one your models use in
`.env.example`:

```sh
ANTHROPIC_API_KEY=sk-ant-...    # or OPENAI_API_KEY, ...
```

Because credentials live in the environment rather than the container filesystem, they
survive image upgrades — `docker compose pull && docker compose up -d` needs no re-login.
Any other env var in the container is inherited by opencode as well, including its own
settings.

## Security

The agent runs with your `GITHUB_TOKEN` and can commit whatever it produces — the manifest
filter is the only gate. On public repos, avoid permissive filters (e.g. trusting any
commenter); that's arbitrary code execution on your host. Prefer label gates that require
triage permission, or restrict by author login. Use a scoped token.

## Known limitations (v0)

- Events are processed in memory; a crash mid-event loses it (GitHub redeliveries are not
  deduplicated).
- The PR is created but nothing is commented back on the triggering issue.
- The opencode CLI is installed at image build time (pin the version for production).

## Development

```sh
go test ./...   # unit + end-to-end integration tests
go vet ./...
```
