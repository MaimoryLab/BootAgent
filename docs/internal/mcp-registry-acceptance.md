# MCP Registry Acceptance Checklist

- [ ] The MCP route renders its existing Registry summary before the background scan completes.
- [ ] Only command-detected Agents with an existing configuration root appear as targets; scanning creates no Agent directories.
- [ ] Claude Code, Codex, OpenCode, Kilo CLI, and Hermes configurations are discovered and written through their native formats.
- [ ] JSONC comments, trailing commas, unknown fields, unrelated MCP IDs, and non-MCP settings survive a targeted Apply.
- [ ] Parse failures retain the previous factual Registry entry and show a redacted diagnostic instead of treating the entry as deleted.
- [ ] Same-ID divergent specs remain conflicts until a user selects an explicit variant.
- [ ] List and scan responses contain no credential values; explicit detail and transfer paths are the only full-spec boundaries.
- [ ] Apply is explicit, serialized with the existing write lock, reports partial success, and keeps failed targets retryable.
- [ ] A native close with a dirty MCP draft asks for discard and cannot recurse when closing after confirmation.
- [ ] Import preview performs no writes, supports collision choices, and still requires Apply after confirmation.
- [ ] Omit-secret export is the default; encrypted and plaintext export require their respective explicit inputs.
