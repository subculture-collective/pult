# Aether template variables

Question (M1 known risks, registry schema): can Aether's template variable names be offered as aliases, so Aether templates work with Theme Tender?

Read from [bjarneo/aether](https://github.com/bjarneo/aether) at `d302f53` (2026-11-01, MIT): `internal/template/variables.go` (`BuildVariables`), `internal/template/engine.go` (`ReplaceVariable`), `internal/color/darken_lighten.go`, `docs/custom-templates.md` and the templates in `templates/`.

## Findings

- **Syntax.** Aether replaces `{name}` and `{name.modifier}` by plain string substitution of known names; other braces stay as they are. Theme Tender's templates are Mustache (`{{name}}`). An Aether template therefore needs converting before this renderer can read it.
- **Names clash.** Aether's `red`, `green`, `yellow` and `blue` are ANSI colors 1–4, and `black` … `bright_white` are ANSI 0–15. Theme Tender's `red`, `green`, `yellow` and `blue` are Catppuccin roles, which differ from the ANSI colors in most themes. Top-level aliases would change the meaning of existing templates, so the Aether names live under `aether.` instead.
- **Variables.** `background`, `foreground`, `cursor`, `selection_foreground`, `selection_background`, the 16 ANSI names and `color0` … `color15`, `accent`, `bg`, `fg`, `muted`, `mode` and `theme_type`, plus derived shades: `dark_bg`, `darker_bg`, `lighter_bg`, `dark_fg`, `light_fg`, `bright_fg`, `orange`, `brown`, `selection`.
- **Derived shades.** `LightenRGB(c, p)` moves each sRGB channel p% of the way to 255 and `DarkenRGB(c, p)` scales it to p%, both rounded. `lighter_bg` is background lightened 10%, `dark_bg` and `darker_bg` background darkened to 75% and 50%, `dark_fg` foreground darkened to 75%, `light_fg` and `bright_fg` foreground lightened 15% and 25%, `orange` red lightened 15%, `brown` orange darkened to 60%. `selection` is `selection_background`, unless the selection colors are just the inverted background and foreground; then it is background lightened 10% (darkened to 85% in light themes). `accent` defaults to ANSI blue; Aether lets the user pick another, which Theme Tender has no equivalent for.
- **Modifiers.** `.strip` (hex without `#`), `.rgb` (`r,g,b`, no spaces, unlike Theme Tender's `.rgb`), `.rgbs` (`r g b`), `.rgba` (`rgba(r, g, b, 1)`), `.rgba:N` (chosen alpha) and `.yaru` (Yaru icon theme name).
- **Usage.** In the 24 files under `templates/`, counting each name with any modifier, the most used are `muted` (175), `accent` (135), `yellow` (128), `foreground` (126), `lighter_bg` (82), `cyan`, `blue`, `red`, `green`, `background` and `orange` (48). Derived shades are common enough that aliasing only the base colors would leave most templates broken.

## Decision

Implemented in `internal/render/aether.go`:

- `{{aether.<name>}}` holds every variable above with Aether's values, and each color has `.strip`, `.rgb`, `.rgbs` and `.rgba` with Aether's formats.
- `render.AetherToMustache` and `tender render -aether <theme> <file>` convert `{name}` to `{{aether.name}}` for known names and leave other braces alone, as Aether does.
- Not supported, and reported as errors: `.rgba:N`, `.yaru`, and templates that already contain `{{` or `}}`. A chosen accent color is not modelled; `accent` is ANSI blue.

Theme Tender's own templates keep using its role names. The aliases are for reusing an Aether template, for example a user's `~/.config/aether/custom` one, with Theme Tender's palettes.
