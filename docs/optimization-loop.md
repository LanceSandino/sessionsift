# Personal optimization loop

The local **Optimize** workspace turns deterministic aggregate patterns into a
deliberate improvement cycle:

1. Choose a recommendation and record whether it is proposed, accepted,
   applied, dismissed, or measured.
2. Start an experiment against one baseline session. SessionSift stores the
   session ID, a redacted user-authored title/hypothesis, and optional
   recommendation ID; it does not copy the transcript.
3. Attach a later follow-up session. The experiment becomes measured and shows
   raw follow-up-minus-baseline deltas for tokens, cache reuse, tool calls,
   failures, findings, active work, and first validation.
4. Download the current seven-day report as Markdown or JSON.

Measurements are descriptive evidence, not a productivity score, quality
judgment, or declared winner. The downloadable JSON omits prompt-derived session
titles and session identifiers. Both exports omit conversation text, assistant
responses, tool arguments, and tool results.

Adoption tracking is local-only and deliberately coarse: active days, Optimize
views, experiments created, recommendation changes, and reports exported. Those
counters contain no session ID, actor, content, or remote telemetry destination.

The loop is personal and single-installation. Team outcome intelligence,
repository-provider correlation, billing, organization policy, and enterprise
identity lifecycle are later product phases, not hidden behavior in this one.
