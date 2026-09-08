# Barney

<p align="left">
  <img src="website/assets/barney-white.jpg" width="360" alt="Barney, a purple T-Rex roaring YOLO">
</p>

**A self-hosted daemon that turns GitHub webhooks into agent runs.**

Barney listens for GitHub events, checks out your repo into a workspace it keeps warm between
runs, and — when a rule in `.barney/manifest.yaml` matches — hands a rendered prompt to an AI
agent (like [opencode](https://opencode.ai)). The agent gets a real git checkout, your GitHub
token, and bash. It commits, pushes, comments, and opens pull requests itself; Barney's job
ends the moment the agent starts.

This is a trade: you run and operate the daemon yourself, instead of using GitHub Actions or a
hosted agent product. In exchange you get a persistent per-repo workspace (no full re-clone
per event), one daemon that can watch many repos under a single set of credentials, and a
trigger config that lives in the repo instead of a settings page.

## Why Barney

- **One file per repo** — `.barney/manifest.yaml` lives in your repo, versioned with your code.
  Change triggers or prompts like any other config, review them in PRs, revert them like any
  other commit.
- **Bash is all you need** — the agent gets a real checkout, your GitHub token, and plain
  `git` + `gh`. Commits, pushes, comments, pull requests are just shell commands the agent
  runs. Build your automation in prompts and `AGENTS.md` files; Barney never has to change.
- **Warm workspace** — each repo keeps a persistent clone under `WORKSPACE_ROOT` instead of a
  fresh checkout per run, so repeated events on the same repo skip the full clone.
- **Your infra, your keys** — self-hosted. Your token and agent credentials never leave your
  environment.
- **Any agent** — pluggable harnesses; ships with opencode out of the box.
- **Event-driven** — reacts to issues, issue comments, pull requests, PR review comments, and
  pushes.

Read the [Security](#security) section before pointing this at a public repo — the manifest
filter controls *who* can trigger a run, not what's *in* the payload the agent reads.

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

Commit this to your repo — it hands every issue **assigned to your bot account** to the agent:

```yaml
# .barney/manifest.yaml
version: "v0"
triggers:
  - id: assigned-to-barney
    event: issues.assigned
    filter: payload.issue.assignees.exists(u, u.login == 'barney-bot')
    agent: opencode
    prompt_template: |
      Issue: {{ .payload.issue.title }}

      {{ .payload.issue.body }}

      Implement the change described above and make sure the tests pass.
```

Replace `barney-bot` with your bot account's login. Now anyone can assign an issue to the
bot and Barney will clone the repo and run the agent on it. (Prefer labels?
`filter: payload.issue.labels.exists(l, l.name == 'agent-task')` with `event: issues.opened`
works the same way.)

## The manifest

A trigger fires when the event matches `event: "<type>.<action>"` *and* the optional CEL
`filter` passes. The `filter` is evaluated against the raw GitHub payload, so you can gate on
anything GitHub sends: labels, authors, titles, draft state, branches. Omit it and the
trigger fires on every matching event. Malformed filters are skipped, not fatal.

Supported events: `issues`, `issue_comment`, `pull_request`,
`pull_request_review_comment`, `push`.

**Label-gated tasks** — anyone with triage rights labels an issue, the agent implements it:

```yaml
  - id: label-task
    event: issues.opened
    filter: payload.issue.labels.exists(l, l.name == 'agent-task')
    agent: opencode
    prompt_template: |
      Issue: {{ .payload.issue.title }}
      {{ .payload.issue.body }}
      Implement the change and keep tests green.
```

**Slash-command on issues** — comment `/barney fix the flaky login test`:

```yaml
  - id: slash-command
    event: issue_comment.created
    filter: payload.comment.body.startsWith('/barney')
    agent: opencode
    prompt_template: "The user wrote: {{ .payload.comment.body }}\nDo what they asked on issue #{{ .payload.issue.number }}."
```

**PR review helper** — post a risk assessment on every opened PR:

```yaml
  - id: pr-summary
    event: pull_request.opened
    filter: '!payload.pull_request.draft'
    agent: opencode
    prompt_template: |
      Review PR #{{ .payload.pull_request.number }}: {{ .payload.pull_request.title }}
      Post a concise risk assessment of the diff as a comment on the PR
      (gh pr comment {{ .payload.pull_request.number }} --repo $BARNEY_REPO --body-file ...).
```

**Respond to pushes on main**:

```yaml
  - id: post-merge
    event: push
    filter: payload.ref == 'refs/heads/main'
    agent: opencode
    prompt_template: "A push landed on main. Run the test suite and open a fix PR if anything fails."
```

## The agent environment

Each event gets its own branch (`barney/<event>-<delivery>`) in a per-repo workspace; events
on the same repo are processed one at a time. The agent runs inside that workspace with:

| Variable             | Meaning                                                  |
| -------------------- | -------------------------------------------------------- |
| `BARNEY_EVENT_TYPE`  | GitHub event type (`issues`, `push`, ...)                |
| `BARNEY_EVENT_ID`    | GitHub delivery ID (unique per event)                    |
| `BARNEY_REPO`        | Repository as `owner/name`                               |
| `BARNEY_BRANCH`      | The event branch checked out in the workspace            |
| `BARNEY_BASE_BRANCH` | Default branch, or the PR base for `pull_request` events |

Git-over-HTTPS is pre-authenticated with `GITHUB_TOKEN` via environment config — no
credentials are written to disk — so `git push` just works inside the workspace, and `gh`
picks up the same token. All other daemon environment variables (agent API keys,
`OPENCODE_*` settings) are inherited too. Whatever the agent does — run tests, fix bugs,
comment, commit, push, open PRs — happens through those tools and is defined by your prompt.

## Configuration

| Environment      | Default                      | Required | Notes                                  |
| ---------------- | ---------------------------- | -------- | -------------------------------------- |
| `WEBHOOK_SECRET` | —                            | yes      | HMAC secret for webhook validation     |
| `GITHUB_TOKEN`   | —                            | yes      | Git-over-HTTPS auth and `gh`; inherited by agents |
| `PORT`           | `8080`                       | no       |                                        |
| `WORKSPACE_ROOT` | `/var/lib/barney/workspaces` | no       | Where repos are cloned                 |
| `EVENT_TIMEOUT`  | `30m`                        | no       | Per-event processing limit             |

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

The agent runs with your `GITHUB_TOKEN` and a bash shell. Two different things gate it, and
they protect against different threats:

- **The manifest filter controls *who* can trigger a run** — e.g. "only when a maintainer
  applies the `agent-task` label."
- **It does not control what's *in* the payload.** An issue title, body, or comment is
  attacker-controlled text that gets rendered straight into the agent's prompt. Anyone who can
  get a trusted actor to satisfy your filter (by commenting, labeling, or assigning) can smuggle
  instructions into that text — this is prompt injection, and the agent's bash access makes it
  as powerful as whatever is in its environment.

What Barney does to limit the blast radius:

- **The manifest is always read from the repository's base branch** (the default branch, or a
  pull request's own base) — never from the event's checked-out working tree. A pull request
  from a fork can still be reviewed by an agent, but it cannot ship its own triggers or prompts;
  only someone with write access to the base branch can change what Barney runs.
- **`WEBHOOK_SECRET` is stripped from the agent's environment.** It has no legitimate use there,
  and leaking it would let an attacker forge future webhook deliveries.

What Barney does **not** do: sandbox the agent, restrict its filesystem or network access, or
inspect prompt content for injected instructions. It runs with the same privileges as the
daemon process.

Practical guidance:

- Use a **scoped token** — limited to the repos Barney manages, not a personal token with
  org-wide access.
- On public repos, gate on **label + triage permission** or a **trusted author allowlist**, and
  still assume the payload body itself is hostile input.
- Treat running Barney like giving a contributor shell access to your CI secrets, because
  that's what it is.

## Known limitations (v0)

- Events are processed in memory; a crash mid-event loses it (GitHub redeliveries are not
  deduplicated).
- No sandboxing: the agent runs with the daemon's full (filtered) environment and network
  access.
- The opencode CLI is installed at image build time (pin the version for production).

## Development

```sh
go test ./...   # unit + end-to-end integration tests
go vet ./...
```

## License

[MIT](LICENSE) — free to use, modify, and ship.