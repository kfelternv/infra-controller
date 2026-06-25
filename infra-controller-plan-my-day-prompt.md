# Infra Controller Plan My Day Prompt

Review my open PRs and assigned issues on `NVIDIA/infra-controller` and produce a concise "plan my day" next-steps brief.

## Goal

Analyze Kyle's existing open PRs and assigned issues in `NVIDIA/infra-controller`.

This automation is advisory only. Kyle is the decision maker. Help Kyle decide what to do next today: what to fix, what to wait on, what to ask, what due dates or timelines matter, and what follow-up prompt to run if AI should investigate or implement something later.

Do not make code changes, commits, pushes, PR comments, issue comments, labels, review requests, CI reruns, branch changes, issue transitions, or new PRs/issues.

## Output Requirements

Send the concise final Slack brief to `kfelter-cursor-automations`.

Do not create, attach, link, or print a detailed appendix.

Do the investigation needed to make good recommendations, but only output the Slack-ready daily plan. If Slack posting fails, leave only the Slack-ready summary in the run output.

Do not include:

- a "Detailed appendix" line
- a "Details" section
- a "Constraints" section
- per-PR or per-issue dossiers
- appendix markdown files

## Output Style

Write directly to Kyle in a natural, helpful daily-planning voice.

The Slack report should feel like a personal planning note, not an audit report and not a third-person report about Kyle.

Use compact Slack links:

- Prefer `<https://github.com/NVIDIA/infra-controller/pull/2359|#2359>`
- Prefer `<https://github.com/NVIDIA/infra-controller/issues/2219|#2219>`
- Do not paste raw URLs in the Slack message unless Slack link formatting is unavailable.

Avoid terse code words or shorthand such as "CI red", "stale?", "owner?", "ACK", "nit", or "needs action".

Use natural phrasing instead, such as:

- "CI is failing, so fix that before asking for another review."
- "Frank asked for a design decision; reply with the preferred direction before changing code."
- "This has been quiet for a week, so send a short ping if you still want it reviewed."

Keep the Slack report compact enough to skim on a phone. Aim for roughly 45 lines. If full follow-up prompts make that impossible, keep the prompts complete and reduce the number of items.

Use exactly these three content sections after the title and reviewed count:

1. `Today's High Level Plan`
2. `How AI Can Help`
3. `Messages For You To Send`

Do not add separate Slack sections for decisions, timeline, waiting, people, details, constraints, or appendix content. Fold those details into the three required sections only when they change today's plan.

## Slack Item Structure

For PRs and issues in Slack, use this structure:

`<compact PR or issue link> / <PR or issue title>`

- `<natural action Kyle can take today>`
- `<brief explanation that requires no prior context>`

Examples:

`<https://github.com/NVIDIA/infra-controller/pull/2359|#2359> / Fix Redfish retry handling`

- Fix the failing integration check before asking for another review.
- The PR is otherwise close, but reviewers are unlikely to re-review while CI is failing.

`<https://github.com/NVIDIA/infra-controller/issues/2219|#2219> / Track node wipe timeout`

- Safe to wait today unless the milestone becomes urgent.
- There is no due date visible, and the related PR already covers the likely fix.

## Today's High Level Plan

This section should answer: "What should Kyle actually focus on today?"

Prioritize by expected progress per unit effort, urgency, and deadline impact.

Include:

- PRs with requested changes
- PRs with failing CI
- stale PRs that need a ping
- assigned issues without an active PR
- issues with due dates, milestones, target releases, security urgency, or customer urgency
- decisions or consensus questions that block forward progress

Timeline and due-date context belongs here when it changes priority. Explain whether something means:

- do this today
- watch this week
- safe to wait
- stale and needs a ping

Group related PRs/issues when useful.

## How AI Can Help

This section should contain the top 2-4 full copy/paste prompts Kyle can run with a fresh AI agent.

Do not write one-sentence summaries here. Each prompt must be complete enough for a totally fresh agent that has no prior context from this run.

Each follow-up prompt must include:

- the repo: `NVIDIA/infra-controller`
- the relevant PR or issue link
- the exact task to investigate or implement
- the evidence found in this planning run
- what files, checks, comments, or CI failures are relevant when known
- read/write expectations for that follow-up task
- instructions to preserve existing behavior
- instructions to run relevant tests or checks
- instructions to commit as Kyle if changes are made
- instructions for safe secret usage

Use this required language in every implementation prompt:

```text
If you make code changes, commit them using the existing git identity configured in the environment, representing Kyle. Do not override git user.name or git user.email. Create a descriptive commit message, push the branch, and create or update the PR if appropriate.
```

Use this required language in every prompt that may need GitHub or other authenticated access:

```text
Use only the configured credentials and secrets already available in the environment, such as GH_TOKEN, GITHUB_TOKEN, or GITHUB_PAT if present. Do not print, echo, log, summarize, or include any secret value in commits, Slack, PRs, issues, reports, shell output, or error messages. If authentication fails, report only the non-secret failure category.
```

Make prompts natural and specific. Avoid vague prompts like "Investigate CI." Prefer:

```text
You are working in NVIDIA/infra-controller on PR #2359: Fix Redfish retry handling.

The planning run found that the PR is blocked by the `cargo make clippy` check and one unresolved reviewer thread from Frank about retry backoff behavior. Inspect the failing check logs, the latest reviewer comments, and the files changed in the PR. Determine whether the failure is caused by this PR or by main branch drift.

If the fix is straightforward, implement it while preserving the existing Redfish retry behavior except for the reviewed change. Run the most relevant Rust tests or checks for the touched crates.

If you make code changes, commit them using the existing git identity configured in the environment, representing Kyle. Do not override git user.name or git user.email. Create a descriptive commit message, push the branch, and create or update the PR if appropriate.

Use only the configured credentials and secrets already available in the environment, such as GH_TOKEN, GITHUB_TOKEN, or GITHUB_PAT if present. Do not print, echo, log, summarize, or include any secret value in commits, Slack, PRs, issues, reports, shell output, or error messages. If authentication fails, report only the non-secret failure category.
```

## Messages For You To Send

Include only ready-to-send human messages that have a concrete ask.

Each message should be short, natural, and directly usable.

Use this structure:

`<person or group>` about `<compact PR or issue link> / <title>`:

```text
<message Kyle can send verbatim>
```

Do not recommend outreach unless there is a specific question, review request, or decision needed.

## Slack Reporting

Send the concise final Slack brief to `kfelter-cursor-automations`.

There may be a Slack channel search/cache issue where `kfelter-cursor-automations` does not appear in search even though it is postable and has worked before.

- Attempt to use the configured Slack posting tool/action for `kfelter-cursor-automations`.
- Do not repeatedly search for the channel.
- Do not post to any other channel, thread, PR, GitHub issue, or comment surface.
- Slack reading is failing at the moment. Do not try to read Slack.
- Always include PR or issue links when referencing them in Slack.
- If Slack posting fails, leave only the Slack-ready summary in the run output.
- Do not ask Slack questions or block for input during this run.
- Do not include a boilerplate "Details" section in Slack.
- Do not include a boilerplate "Constraints" section in Slack.

## GitHub Authentication

Use the configured GitHub token for read-only GitHub CLI/API access.

Before listing PRs or issues, check GitHub CLI authentication.

If `gh` is not authenticated but the environment contains `GITHUB_PAT`, use it as the GitHub CLI token by setting `GH_TOKEN=$GITHUB_PAT` for GitHub CLI commands in this run.

If `GH_TOKEN` or `GITHUB_TOKEN` is already set, prefer the existing GitHub CLI-supported variable.

Do not print, log, echo, summarize, or include any token value in Slack, run output, shell output, reports, or error messages.

If GitHub authentication still fails, report the exact non-secret failure category in the Slack brief and run output, such as:

- missing token
- insufficient token scope
- SSO authorization required
- expired token
- GitHub CLI unavailable

Continue with any read-only evidence available from local checkouts or public metadata, but clearly mark confidence as low where GitHub evidence could not be inspected.

## Repository And Issue Scope

Work only with `NVIDIA/infra-controller`.

Inspect:

- Kyle's open PRs in `NVIDIA/infra-controller`
- all open GitHub issues assigned to Kyle in `NVIDIA/infra-controller`
- issues directly linked from Kyle's PRs
- issues mentioned in PR bodies, commits, branch names, review comments, or check output
- referenced Jira tickets, NVBugs, or other tracker items assigned to Kyle or materially connected to Kyle's PRs, if visible through configured read-only tools

Treat all GitHub and issue tracker operations as read-only.

Do not:

- create branches
- check out PR branches unless needed for read-only inspection
- edit files
- commit
- push
- open, close, merge, mark ready, request review, edit descriptions, label, rerun CI, comment on PRs/issues, assign issues, transition issues, or create PRs/issues

## Read-Only Investigation

For each open PR, inspect enough evidence to make a useful recommendation:

- PR title, number, URL, author, head branch, base branch
- review decision
- unresolved review threads
- requested changes
- failing, pending, or skipped checks
- CI check names and URLs when available
- latest reviewer activity date
- latest Kyle commit date
- PR opened date and updated date
- recent commits
- files changed
- review comments and inline comments
- linked or mentioned issues
- CODEOWNERS if relevant
- recent commit or blame history only when useful for consensus suggestions

For assigned issues, inspect:

- issue title, URL, tracker, state, assignee, labels, priority, milestone, due date, target release, created date, and updated date
- linked PRs or PR references
- whether the issue is blocked, stale, urgent, waiting on someone, or already addressed by an open PR
- whether the issue changes the priority, urgency, sequencing, or recommended next action for any PR

Prioritize PRs and issues with:

- explicit due dates, milestones, target releases, or security/customer urgency
- `CHANGES_REQUESTED`
- failing CI
- unresolved review threads
- stale discussion or unclear owner
- large or risky file changes
- assigned issues with no active PR
- multiple possible solution paths

## Judgment Rules

Rank work by expected progress per unit effort, urgency, and deadline impact, not just severity.

Distinguish:

- code changes Kyle could approve an AI to make
- decisions Kyle needs to make
- consensus Kyle needs from another person
- PRs that only need a reply, re-review, or waiting for CI
- issues that already appear covered by an open PR
- issues that need a new implementation plan
- work where no action is recommended today

For reviewer comments, say whether feedback appears already addressed by later commits.

For assigned issues, say whether an open PR appears to address the issue or whether there is no visible implementation path yet.

Do not recommend reaching out to someone unless there is a concrete question for them.

If ownership or consensus targets are inferred from commit history, say "inferred from recent commits" and keep the evidence brief.

Do not invent certainty. Use confidence: high / medium / low.

Do not invent due dates. If no due date or milestone is visible, omit it from the Slack brief unless the absence itself matters.

## Slack Brief Template

Use this exact high-level structure for the Slack message.

# Infra Controller Daily Plan

Reviewed: `<number>` open PRs, `<number>` assigned issues

## Today's High Level Plan

1. `<compact PR/issue link> / <title>`
   - `<natural action Kyle can take today>`
   - `<brief explanation that requires no prior context>`

2. `<compact PR/issue link> / <title>`
   - `<natural action Kyle can take today>`
   - `<brief explanation that requires no prior context>`

3. `<compact PR/issue link or grouped links> / <title or theme>`
   - `<natural action Kyle can take today>`
   - `<brief explanation that requires no prior context>`

## How AI Can Help

- `<compact PR/issue link> / <title>`:

```text
<full copy/paste prompt for a fresh agent. Include repo, PR/issue link, evidence from this run, exact task, relevant files/checks/comments/CI failures, read/write expectations, checks to run, preserve-existing-behavior instruction, commit-as-Kyle instruction, and safe-secret-usage instruction.>
```

- `<compact PR/issue link> / <title>`:

```text
<full copy/paste prompt for a fresh agent. Include enough context for an agent that has not seen this planning run.>
```

## Messages For You To Send

- `<person or group>` about `<compact PR/issue link> / <title>`:

```text
<short ready-to-send message with a concrete ask>
```

- `<person or group>` about `<compact PR/issue link> / <title>`:

```text
<short ready-to-send message with a concrete ask>
```
