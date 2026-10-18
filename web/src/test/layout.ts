// jsdom lays nothing out, so every measurement is an empty box; CodeMirror measures ranges and asks which element
// is at a point. Adapted from Exciter's web/src/test/layout.ts (see NOTICE).
const noRects = (): DOMRectList => Object.assign([], { item: () => null }) as unknown as DOMRectList;
Range.prototype.getBoundingClientRect ??= () => new DOMRect();
Range.prototype.getClientRects ??= noRects;
document.elementFromPoint ??= () => null;
