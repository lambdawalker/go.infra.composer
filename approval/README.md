# Deployment approvals

`approval.Policy` is a UI- and provider-independent approval policy. Its zero value requires an explicit answer. Applications choose a timeout only when opting into timed mode; Composer has no process-global switch or fixed timeout.

```go
policy := approval.Policy{Mode: approval.Timed, Timeout: 5 * time.Second}
err := policy.Confirm(ctx, approval.Routine, func(promptCtx context.Context) (bool, error) {
    return terminalPrompt(promptCtx) // application-provided UI
})
```

Pass `Sensitive` or `Destructive` for operations that must always require an explicit answer, even under a timed policy. Applications classify their operations, own CLI flags and terminal rendering, and translate successful approval into deployment-tool arguments.

The prompt must honor its context, return `false, nil` for rejection and the unwrapped `ctx.Err()` when that context ends. Keep terminal/input failures as errors. The policy auto-approves only when its own routine-approval deadline expires and the prompt returns exactly `context.DeadlineExceeded`. Parent cancellation/deadlines, explicit rejection and unrelated errors never count as approval. Wrapped or joined errors are propagated so an accompanying terminal failure cannot be hidden by a deadline. The policy returns `ErrRejected` for rejection and creates no background prompt goroutine.

Do not translate every UI-library timeout into `context.DeadlineExceeded`: some libraries use timeout errors for terminal failures as well. An adapter may translate one only after verifying that the context passed to its prompt has actually expired. It must preserve user-abort errors. Prompts that cannot honor cancellation cannot provide timely unattended approval; use a compatible UI adapter instead of leaving a background reader running.

This package does not change teardown, executor, or CI behavior by itself. Callers opt into it at their existing approval boundary.
