# Image Preprocessing Pipeline

`internal/image` runs once on image attach (clipboard paste or `@image:/path`
expansion). Pipeline stages, in order:

1. **Resize** — `ResizeImage(data, mime)`. If the long edge exceeds
   `MaxLongEdge` (1568 px) the image is downscaled with Lanczos resampling
   (`disintegration/imaging`) preserving aspect ratio. Below the threshold the
   input bytes pass through unchanged.

2. **Format selection** — `SelectFormat(img)` returns:
   - `image/png` when the image has alpha channel pixels (sampled corners +
     centre — any alpha < 0xFFFF means transparent).
   - `image/png` when the image looks like a screenshot/diagram (cheap
     heuristic: <16 distinct colours sampled across ≤100 pixels).
   - `image/jpeg` otherwise. JPEG quality is fixed at 90.

3. **Metadata strip** — `StripMetadata(data, mime)` decodes and re-encodes
   without preserving EXIF, ICC profiles, or any non-pixel markers.

4. **Token estimation** — applied at the call site (`internal/tui`,
   `estimateTokens(w, h, provider)`):
   - Anthropic / Claude / DeepSeek anthropic-wire: `(w*h)/750`
   - OpenAI gpt-4o / gpt: tile-based `ceil(w/512) * ceil(h/512) * 170 + 85`
   - Unknown providers: `-1`, surfaced as `≈ unknown` in the badge

The 1568 long-edge threshold matches Anthropic's recommended cap and is
acceptable for OpenAI's tile model — both providers accept these images
without further server-side resize.
