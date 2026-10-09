# AGENTS.md

Guidance for coding agents working in this repo (`go-scaffold`, a go-zero based microservice scaffold).
Human-facing docs live in [README.md](./README.md); error-code rules in [pkg/errors/error-code-spec.md](./pkg/errors/error-code-spec.md); SDK contract in [integration-spec.md](./integration-spec.md).

## Agent skills

### Issue tracker

Issues and specs live as GitHub issues in `ShenHaoCore/go-scaffold`; use the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical roles — `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix` — each label string equal to its role name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` + `docs/adr/` at the repo root (both created lazily). See `docs/agents/domain.md`.
