# PLANS.md

Plans are versioned artifacts here.

## When To Use A Lightweight Plan

Use task-thread notes only when the change is small, local, and can be verified in one pass.

Examples:

- doc fix
- one-package bug fix
- targeted test addition

## When To Use An Execution Plan

Create a checked-in plan under `exec-plans/active/` when work has any of these:

- multiple packages or contract surfaces
- migration or config changes
- uncertain external behavior
- meaningful rollback or safety considerations
- work likely to span more than one session

## Required Sections For Execution Plans

- goal
- scope
- non-goals
- risks
- step list
- decision log
- verification notes

## Lifecycle

1. Create in `exec-plans/active/`
2. Update progress and decision log while working
3. Move to `exec-plans/completed/` when done
4. Add leftover gaps to `exec-plans/tech-debt-tracker.md`

## Current Gap

Plan hygiene is documented, but not yet mechanically enforced by CI. Add doc linting and stale-plan checks before the repo gets larger.
