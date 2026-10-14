# Short Report

A tiny local web app that shows today's date and an LLM-written news
briefing — filtered to the topics you care about, compressed to as many
(or as few) words as you want.

- Pick topics (World, USA, New York, Soccer, War, or anything you type in)
- Drag the length slider from a one-line takeaway up to "∞ — Everything"
  (every fetched headline, unsummarized)
- The briefing regenerates automatically whenever you change topics or the
  slider, so it always matches what's currently selected
- Works with Anthropic (Claude), OpenAI (ChatGPT), Google Gemini, or a local
  Ollama model — your choice, configured from the Settings dialog in the app
- Local user accounts, each with their own topics and settings

The Go backend fetches live headlines from public RSS feeds (BBC, NYT, and
a Google News search fallback for anything without a curated feed), then
asks an LLM to synthesize only what matters into your chosen word budget.

## Requirements

- Go 1.23+
- Node.js 18+ / npm (only needed to build the frontend, not to run the
  compiled binary)

Note: the Go binary embeds `web/dist`, so the frontend must be built before
`go build` will succeed.

## Quick start

```sh
make run
```

This builds the React frontend, compiles the Go binary (which embeds the
built frontend), and starts it. Open `http://127.0.0.1:8787` in your browser and create an
account.

Without `make`, the equivalent is:

```sh
cd web && npm install && npm run build && cd ..
go build -o short-report .
./short-report
```

## Adding an LLM provider

Nothing works without one of these — until you set one up, the app falls
back to showing raw, unsummarized headlines with a note explaining why.

**Hosted APIs (Anthropic, OpenAI, Gemini)** — either set an environment
variable before launching:

```sh
export ANTHROPIC_API_KEY="sk-ant-..."   # or OPENAI_API_KEY / GEMINI_API_KEY
./short-report
```

or open **Settings** in the app and paste your key there (it's saved in the
local database, not sent anywhere except the provider's API).

**Local model via Ollama** — install [Ollama](https://ollama.com), pull a
model, and switch the provider in Settings:

```sh
ollama pull llama3.2
```

Then in Settings choose "Local (Ollama)" and set the model name to match.

## Development

Two terminals, for hot-reloading the frontend while the Go backend serves
the API:

```sh
go run .                # backend on :8787
cd web && npm run dev   # frontend on :5173, proxies /api to :8787
```

Visit `http://localhost:5173` while developing. For a production-style
single-binary run, use `make run` instead.

## Project layout

```
main.go                     entry point, embeds web/dist
internal/auth/              session management
internal/config/            user accounts + settings in SQLite (OS user-config
                            dir, e.g. ~/.config/short-report), env overrides
internal/news/              RSS fetching, tag→feed mapping, caching
internal/llm/               Anthropic, OpenAI, Gemini, Ollama providers
internal/server/            HTTP handlers (/api/auth/*, /api/config,
                            /api/aggregate, /api/ollama/models)
web/                        React + Vite frontend (source in web/src)
```

## Notes

- The server binds to `127.0.0.1` only by default — not reachable from
  other machines on your network. Change `-addr` if you really want that.
- Accounts (bcrypt-hashed passwords) and per-user settings (topics, length,
  provider, model, and — if you typed one in the UI rather than via env var —
  API keys) are stored in a local SQLite database, `shortreport.db`, in a
  `0700` directory under your OS user-config dir.
- `when:1d`-scoped Google News searches and live RSS feeds mean "today's
  news" is genuinely today's; there's no static or cached dataset involved.

## License

MIT — see [LICENSE](LICENSE).
