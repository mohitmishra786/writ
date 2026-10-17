// Writ's AG Studio theme. Parameter names are from AgStudioThemeParams in
// ag-studio 3.0.0. Values reference CSS variables (styles.css) that come from
// the dataviz reference palette: warm-neutral surfaces, hairline borders,
// near-black ink, and the categorical slots in their validated fixed order
// (slot 1 blue leads; charts here are single-series, so only blue appears).

import { studioTheme } from 'ag-studio';

const v = (name: string) => `var(--${name})`;

const palette = Object.fromEntries(
  Array.from({ length: 8 }, (_, i) => [
    [`chartPaletteFills${i + 1}Color`, v(`series-${i + 1}`)],
    [`chartPaletteStrokes${i + 1}Color`, v(`series-${i + 1}`)],
  ]).flat(),
);

export const mandateTheme = studioTheme.withParams({
  fontFamily: v('font'),
  textColor: v('ink'),
  foregroundColor: v('ink'),
  subtleTextColor: v('ink-2'),
  backgroundColor: v('surface'),
  borderColor: v('hairline'),
  accentColor: v('series-1'),
  rowHoverColor: v('wash'),
  studioWrapperBorder: false,
  studioCanvasBackgroundColor: v('page'),
  studioCanvasPadding: 20,
  studioCanvasRowHeight: 16,
  studioWidgetBackgroundColor: v('surface'),
  studioWidgetBorder: { width: 1, color: v('hairline') },
  studioWidgetBorderRadius: 6,
  studioWidgetPadding: 14,
  studioWidgetTitleFontSize: 13,
  studioWidgetTitleFontWeight: 600,
  studioWidgetSubtitleFontSize: 12,
  ...palette,
});
