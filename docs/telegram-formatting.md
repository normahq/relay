# Telegram Message Formatting

Relay sends final assistant responses to Telegram with the configured
`relay.telegram.formatting_mode`.

Allowed values:

- `markdownv2` (default): agent output is normal Markdown or plain text. Relay converts it to Telegram MarkdownV2 and sends with `parse_mode=MarkdownV2`.
- `html`: agent output is Telegram HTML. Relay escapes unsafe raw text while preserving supported Telegram HTML tags and sends with `parse_mode=HTML`.
- `none`: Relay sends raw text without `parse_mode`.

Relay follows Telegram Bot API formatting rules:
<https://core.telegram.org/bots/api#formatting-options>

## MarkdownV2 Mode

Use `markdownv2` when agents should write natural Markdown. This is the default and recommended mode.

Supported input:

- plain text and paragraphs
- headings
- bold, italic, strike, and inline code
- fenced code blocks
- links
- blockquotes
- unordered, nested, and ordered lists

Relay behavior:

- Converts normal Markdown/plain text to Telegram MarkdownV2.
- Escapes Telegram MarkdownV2 reserved characters in generated payloads.
- Trims converter-added leading and trailing newlines.
- Normalizes converter list indentation.
- Preserves fenced code block content.
- Tolerates common accidental pre-escaped punctuation, but agents should not rely on this.

Not supported or not recommended:

- Do not ask agents to write raw Telegram MarkdownV2 syntax.
- Do not pre-escape Telegram MarkdownV2 reserved characters in agent instructions.
- Do not rely on exact rendered bullet glyphs; Relay may normalize list markers for Telegram.
- Do not rely on raw Telegram entity syntax in Markdown mode.

Example model output:

~~~markdown
**Build:** success

- Run `relay start`
- Check logs

```bash
go test ./...
```
~~~

## HTML Mode

Use `html` when agents should write Telegram HTML directly. Relay preserves supported Telegram HTML and escapes unsafe raw text.

Supported tags and attributes:

- `<b>`, `<strong>`
- `<i>`, `<em>`
- `<u>`, `<ins>`
- `<s>`, `<strike>`, `<del>`
- `<tg-spoiler>`
- `<span class="tg-spoiler">`
- `<a href="...">`
- `<code>`
- `<pre>`
- `<pre><code class="language-...">...</code></pre>`
- `<blockquote>` and `<blockquote expandable>`
- `<tg-emoji emoji-id="...">`
- `<tg-time unix="..." format="...">`; `format` is optional

Relay behavior:

- Preserves supported Telegram HTML tags.
- Preserves only supported attributes for supported tags.
- Drops unsupported attributes on supported tags.
- Escapes unsupported tags as visible text.
- Escapes raw `<`, `>`, and `&` in text.
- Preserves Telegram-supported entities: `&lt;`, `&gt;`, `&amp;`, `&quot;`, decimal numeric entities, and hex numeric entities.

Not supported:

- Arbitrary HTML tags such as `<div>`, `<script>`, tables, images, and styles.
- Event handlers, CSS classes other than supported Telegram classes, inline styles, and custom attributes.
- Standalone `<code class="language-...">`; language classes are preserved only inside `<pre>`.
- `<tg-time datetime="...">`; use `unix` and optional `format`.
- Custom named HTML entities such as `&copy;`; use numeric entities when needed.

Example model output:

```html
<b>Build:</b> success.
Run <code>relay start</code>.

<pre><code class="language-bash">go test ./...</code></pre>
```

## None Mode

Use `none` when the response must be delivered exactly as raw text.

Relay behavior:

- Omits Telegram `parse_mode`.
- Does not escape Markdown or HTML.
- Does not preserve formatting semantics.

This mode is useful for debugging malformed payloads or sending literal markup.
