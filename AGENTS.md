# Agent Guidelines

- Do **not** include chat, conversation, or workspace links in PR titles or descriptions.
- Use branches named `feat/<area>-<change>` or `fix/<area>-<change>` for new work.
- When a change affects HTTP endpoints, add a brief **How to verify** section to the PR body describing which endpoint to hit.
- For docs-only updates, record the test status as `Tests: not run (docs-only).`
- If a change spans both `elora-chat` and `gnasty-chat`, call out the shared `/data/` handoff in the PR summary.
