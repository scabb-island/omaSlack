---
name: ui-polish
description: >
  Review and polish omaslack's visual interfaces (the QML plugin: bar icon and
  badges, quick panel, full window; and the TUI) without changing behavior.
  Report first, edit only after the user approves the plan. Use before any
  visual change, when asked to review or polish the UI, or for design issues.
---

# ui-polish

A visual review and polish workflow. It runs together with `ub-mode`: ub-mode
governs how code changes are made (TDD, small commits, quality gates); this
skill governs what a visual change should achieve and the approval gate
before it.

## Tools

It uses the `interfaces` plugin from `jakubkrehel/skills`. The user installs
it once in Claude Code (slash commands only the user can type):

    /plugin marketplace add jakubkrehel/skills
    /plugin install interfaces@interfaces

That provides `/interfaces:better-interface` (audit) and
`/interfaces:better-ui` (polish). If they are not available, follow the same
process by hand and say that the plugin is missing.

## Scope

The scope is a surface, flow or feature: the bar icon and its badges
(`plugin/BarIcon.qml`, `plugin/lib/badges.js`), the quick panel
(`plugin/QuickPanel*.qml`, `plugin/lib/quickpanel.js`), the window
(`plugin/Window.qml` and its parts) or the TUI (`internal/tui`). If the scope
is unclear, ask before starting.

## Process (mandatory)

1. Audit: `/interfaces:better-interface full <scope>`. Look at the real
   thing: renders from the offscreen smoke harness in light and dark Omarchy
   themes, and, with the user's consent, screenshots of the live shell.
2. Turn the audit into a prioritized plan.
3. Stop. Show the report and the plan and wait for approval. Edit no file
   before that.
4. Apply only the approved changes with `/interfaces:better-ui`, in small,
   verifiable, reviewable increments (each one an ub-mode change with its
   tests: pure logic in `plugin/lib/*.js` with qmltestrunner, smokes, TUI
   goldens).

## Design goal

Better visual quality, clarity and consistency without changing behavior,
business logic, state, daemon calls or the data model. The interface should
feel intentional, coherent and native to Omarchy, not generic.

## What to review, in priority order

1. Visual hierarchy and the primary action: primary, secondary and tertiary
   are unambiguous; destructive actions look destructive.
2. Layout and alignment: margins, padding, spacing on the existing scale
   (`Style.space`), optical alignment of icons, badges, buttons and text.
3. Typography: scale, weights, line height, truncation, hierarchy and
   contrast; no new typeface (fonts come from `Style.font`).
4. Color and contrast: only theme tokens (`Commons.Color`, the bar's colors,
   `plugin/lib/contrast.js` helpers); consistent semantic colors for
   success, warning, error, info and neutral; readable in light and dark
   themes.
5. Components: buttons, inputs, lists, overlays, empty and loading states;
   consistent sizes, padding, radius, borders and icons; correct hover,
   focus, active, disabled, loading, error and success states.
6. Badges, chips, tags and status labels: inventory every one; remove the
   decorative or redundant; unify padding, height, radius, type, icon size
   and color; semantic colors with enough contrast; no emoji in badges; a
   badge that carries no real information becomes quiet text or goes away;
   no new badges unless they carry a useful state or number.
7. Icons: optical size, alignment, weight, color, states and one consistent
   icon style (the shell's glyph font).
8. Microinteractions and motion: subtle, functional and consistent, using
   the shell's `Style.duration`; no decorative animation.
9. Accessibility: contrast, visible focus, keyboard order and full keyboard
   use (spec gui-client, "Full keyboard use").
10. Sizes: bar orientations (horizontal and vertical), display scales (1.0,
    1.2, 2.0), narrow and wide windows, the terminal sizes the TUI goldens
    use.

## Strict rules

- Do not change branding, identity or the base palette without a reason and
  the user's confirmation.
- Use existing tokens, components and conventions (`qs.Ui`, `Style`,
  `Commons.Color`, the TUI theme roles); no new design system or dependency
  without asking.
- Prefer small, consistent, low-risk changes over redesigns.
- Do not break behavior, accessibility or keyboard use.
- For every proposed change give the file, component, problem, exact change
  and risk.
- Decisions that belong to the product are questions for the user, not
  choices to make silently.
- Everything written for the project stays in English (CLAUDE.md); the
  report to the user follows the conversation's language.

## Report format

1. Executive summary: at most 5 problems, by impact.
2. Audit findings: accessibility, layout, writing and microcopy, typography,
   color, UI polish, motion.
3. Prioritized problems: P0 (blocks use or accessibility), P1 (consistency,
   hierarchy or visual quality), P2 (optional polish).
4. Change plan, per change: priority, file and component, problem, proposed
   change, risk, how to validate.
5. Polish proposal with exact values: radius, shadows, spacing, optical
   alignment, icon states, animations and transitions.
6. Badges table: current badge, location, problem, decision (keep, modify,
   remove), final proposal, rationale.
7. Questions for the user: only decisions you cannot make.
8. Verification: files to review, states to try, sizes and scales to check,
   accessibility to verify.
