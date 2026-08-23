# Brain Handoff Policy

Host executor switching preserves the visible Brain chat.

## Rules

- Treat a host executor switch as a host replacement, not a new conversation.
- Load current.md and durable Work before continuing, and inspect open delegated Sessions.
- Keep handoff prompts private and reset transcript baselines so bootstrap text is not shown as an assistant reply.
- Preserve Brain's plan, typed Event obligations, and durable next actions across the handoff.
- Continue in the user's current language and do not mention the handoff unless asked.
