# Console design system

The console implements **shadcn/ui New York v4** with native Rust/Dioxus components and CSS. The visual and behavioral reference is [shadcn-ui/ui](https://github.com/shadcn-ui/ui), at commit `dd41d19e2f80ff6891951f7ced74c2c9a650b065`.

Authoritative sources:
- [Neutral theme](https://github.com/shadcn-ui/ui/blob/dd41d19e2f80ff6891951f7ced74c2c9a650b065/apps/v4/app/globals.css)
- [New York components](https://github.com/shadcn-ui/ui/tree/dd41d19e2f80ff6891951f7ced74c2c9a650b065/apps/v4/registry/new-york-v4/ui)

## Implementation contract

- Rust/Dioxus owns component rendering and interaction. No React, TypeScript, Tailwind build toolchain or Radix runtime is required.
- `assets/main.css` owns the shadcn neutral design tokens and the exact base geometry of shared components. Do not reintroduce a second set of theme colors.
- `src/components.rs` owns shared card, badge, alert and icon primitives. Dioxus classes correspond to the upstream component slots, using native Rust RSX attributes supported by the pinned framework.
- Dioxus view modules compose the shared primitives and class variants; ordinary controls must use the canonical classes rather than local hard-coded padding, radii, colors or shadows.
- Icons follow Lucide's 24×24 view box, two-pixel stroke, round caps and round joins.
- Preserve native focus behavior, keyboard operation, visible focus indicators, form labels, input errors, disabled states and `aria-*` semantics. Copying CSS alone is not a substitute for the accessibility behavior of the upstream component.
- Prefer the upstream New York variants: primary, secondary, outline, ghost and destructive buttons; default and secondary badges; outline inputs; rounded-xl cards; neutral tables, tabs and alerts; collapsible/sidebar navigation.
- Responsive layouts must retain the upstream spacing and state changes where a shadcn analogue exists.

## Visual and functional acceptance

For each shipped view, compare its common components against the pinned upstream component in both light and dark mode at desktop and mobile widths. Verify icon stroke, typography, token values, spacing, radius, shadows, hover/focus/disabled styles, overflow, keyboard navigation and screen-reader labels. Document intentional differences only when application-specific content or interaction warrants them.

`THIRD_PARTY_NOTICES.md` retains the upstream license attribution.
