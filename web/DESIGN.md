# DESIGN.md - 艺数智策

## Context (inferred from the existing product)

- Artifact type: SaaS application with an operational scheduling and analytics dashboard.
- Positioning: creative and utilitarian.
- Audience: Chinese-speaking arts teams coordinating content production and publishing. Primary action: review a recommendation and apply a schedule.
- Adjectives: editorial, grounded, precise, team-trusting.
- Visual word translations: editorial -> Chinese serif page titles; grounded -> warm paper surfaces; precise -> compact labels and aligned calendar dates; team-trusting -> explain data sources and keep actions reversible.
- Aesthetic essence: paper, stage, ledger.
- Single-minded proposition: help a small arts team make its own content history actionable.
- References: the app's existing seal mark, content ledger, and monthly calendar. Avoid generic AI gradients and invented performance imagery.
- Mode: light. Density: balanced, with denser schedule and metrics views.
- Constraints: vanilla TypeScript and CSS; Chinese labels; team-scoped data; recommendations must identify source tags and historical sample count; schedule changes need confirmation and an audit trail.

## Aesthetic

- Direction: paper-and-ink operations journal, extending the existing application style.
- Defining trait: group evidence, editable schedule fields, and the apply action in one left-aligned recommendation card.
- Signature move: a compact day badge anchors each recommendation to the calendar while the shared-Tag evidence stays visible in the same card.

## Typography

- Display: Noto Serif SC, falling back to Songti SC and serif; system-provided fonts, no remote font dependency.
- Body: DM Sans, falling back to PingFang SC, Microsoft YaHei, and sans-serif; system-provided fonts, no remote font dependency.
- Scale: deliberately tight for an operations app; 27px page title, 15px panel heading, 14px body, 12px controls, 11px recommendation metadata. Dense details do not drop below 11px in recommendation cards.
- Weights: 400/500/600/700. Left align narrative and recommendation evidence; use tabular numerals for counts.

## Color

- Strategy: warm paper surfaces with slate-blue actions and a restrained clay-red accent. This continues the existing arts-focused palette without an AI-gradient treatment.
- Distribution: approximately 60 neutral / 30 slate and paper surfaces / 10 clay accent.
- Palette (role -> OKLCH | hex):
  - bg: oklch(0.934 0.018 89.4) | #eee9dc
  - surface: oklch(0.982 0.009 93.6) | #fbf9f2
  - fg: oklch(0.292 0.014 76.3) | #302b24
  - muted: oklch(0.580 0.020 78.2) | #81796d
  - border: oklch(0.881 0.023 84.6) | #dfd7c7
  - accent: oklch(0.408 0.034 253.5) | #3d4b5c
  - accent-fg: oklch(0.982 0.009 93.6) | #fbf9f2
  - success: oklch(0.557 0.054 135.0) | #667b5b
  - warning/error: oklch(0.533 0.127 41.1) | #a84f2c
- Dark mode: not currently implemented.

## Spacing, radius, shadow

- Spacing base: 4px; use 8px for related controls and 13-18px between card groups.
- Radius: 5-7px for controls and evidence chips; 10px for panels.
- Shadow approach: soft elevation for existing dashboard panels. Recommendation cards keep the existing panel surface treatment and do not add another shadow layer.

## Layout and composition

- Grid: existing responsive dashboard grid; recommendation cards use two columns on wide screens and one column on mobile.
- Spacing rhythm: tight within metadata and form groups, with a larger break before each evidence block and the apply action.
- Signature layout move: keep recommendation evidence adjacent to date, time, priority, and assignee controls.
- Density: balanced. Scan from source and evidence to the proposed date, then to the confirmed apply action.
- Responsive: desktop-first existing shell with mobile breakpoints at 760px and 480px; recommendation cards collapse to one column.

## Components and states

- Button hierarchy: slate-blue primary apply/generate actions; outlined secondary controls. Generation exposes loading and error feedback; apply and revert require confirmation.
- Inputs: visible labels for date, time, priority, and assignee; server validates workflow and scheduling rules on apply. Preserve entered values after server errors.
- Tables/calendar: left-align labels; keep date and numeric values scannable; use light separators and text labels alongside priority colors.
- Overlays: existing confirmation dialog for apply/revert, with focus and error behavior shared with the application.
- Empty/loading/error: backend-derived no-candidate count, inline loading state, and retryable error state.
- Focus ring: visible 2px slate-blue outline on recommendation inputs and buttons.

## Motion

- Duration: use existing short 200ms control transitions.
- Recommendation motion: no new entrance or layout animation. Reduced-motion mode removes recommendation button transitions and hover movement.

## Iconography

- Set: existing text glyphs and the brand seal. Keep schedule priority and status understandable without glyph or color alone.

## Imagery and illustration

- Mode: real product UI and data labels; no added illustration or stock imagery.
- Rules: do not invent example metrics or imply external-platform data collection.

## Accessibility

- Contrast: palette values are inherited from the existing product; this feature adds no text over new colored backgrounds. A full contrast audit is not part of this feature verification.
- Focus: visible in recommendation-card controls.
- Keyboard: date, time, select, and button controls use native keyboard interaction; drag scheduling retains an edit-button alternative.
- Color independence: priority and eligibility use text labels as well as styling.
- Reduced motion: recommendation button hover transitions are disabled when requested.

## Tokens (existing source of truth)

```css
:root {
  --paper: #eee9dc;
  --paper2: #f6f2e8;
  --card: #fbf9f2;
  --ink: #302b24;
  --muted: #81796d;
  --line: #dfd7c7;
  --blue: #3d4b5c;
  --red: #a84f2c;
  --green: #667b5b;
  --serif: 'Noto Serif SC', 'Songti SC', serif;
  --sans: 'DM Sans', 'PingFang SC', 'Microsoft YaHei', sans-serif;
}
```

- Adapter: plain CSS custom properties in `src/styles.css`.

## Cards and surfaces

- Recommendation cards reuse the existing panel border, radius, surface, and elevation. Evidence and controls remain in one card; do not nest additional cards.

## Slop audit

- Date: 2026-10-16 | Result: pass for the scheduling feature's source-level review; rendered contrast and layout require a browser check.
- Notes: retained the product's seal/paper/slate/clay identity; the feature has no gradient, decorative illustration, or fabricated metrics. Recommendation details are 11px or larger, controls are labeled, and server eligibility is described as server-validated. A screenshot check is still required before claiming rendered-page validation.

## Changelog

- 2026-10-16: documented the existing product direction and the recommendation-card evidence-to-calendar layout; clarified backend-owned eligibility and reduced-motion behavior.
