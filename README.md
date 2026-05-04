# ChordFinder (ultimate-guitar-scraper)

Listens to audio from your microphone or radio, identifies the song, and displays the chords and tab notation. Works as a CLI tool on desktop or a web app on any phone browser.

## How it works

```
mic / radio
     │
     ▼
  ffmpeg records audio
     │
     ▼
  AcoustID (free, unlimited)  ──miss──▶  AudD (100/month free)
     │
     ▼
  Ultimate Guitar API  →  best chord tab
     │
     ▼
  Supabase cache (repeat songs are instant)
     │
     ▼
  chords + tab displayed
```

## Prerequisites

- **Go 1.17+** — to build the CLI
- **ffmpeg** — audio capture and conversion
  - Linux: `apt install ffmpeg`
  - macOS: `brew install ffmpeg`
- **fpcalc** (Chromaprint) — audio fingerprinting for AcoustID
  - Linux: `apt install libchromaprint-tools`
  - macOS: `brew install chromaprint`

## API Keys

| Service | Used for | Cost | Sign up |
|---------|----------|------|---------|
| AcoustID | Song detection (primary) | Free, unlimited | [acoustid.org/login](https://acoustid.org/login) |
| AudD | Song detection (fallback) | 100/month free | [audd.io](https://audd.io) |
| Supabase | Tab result cache | Free tier | [supabase.com](https://supabase.com) |

At least one of AcoustID or AudD is required.

## CLI Usage

### Build

```sh
go build -o chordfinder .
```

### Listen (main feature)

```sh
export ACOUSTID_API_KEY="your_key"
export AUDD_API_KEY="your_key"       # optional fallback

./chordfinder listen                  # 5-second recording, loops
./chordfinder listen --duration 8    # longer recording
./chordfinder listen --type tabs     # prefer tab notation over chords
./chordfinder listen --no-chords     # skip chord diagram summary
```

Press **Enter** to listen again, **Ctrl+C** to quit.

### Other commands

```sh
./chordfinder fetch -id 96835              # fetch a tab by ID
./chordfinder export -id 96835             # export tab as HTML
./chordfinder wav -id 113039 -output out.wav  # export tab as WAV
./chordfinder get_all --output ./tabs      # download all your saved tabs (requires login)
```

## Web App

Anyone with a phone browser can use it — no install required.

### Deploy to Railway

1. Push this repo to GitHub
2. Go to [railway.app](https://railway.app) → New Project → Deploy from GitHub
3. Select the repo — Railway will detect the `Dockerfile` and build automatically
4. Set environment variables (see below)
5. Go to **Settings → Networking → Generate Domain** to get your public URL

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ACOUSTID_API_KEY` | one of these two | AcoustID client key |
| `AUDD_API_KEY` | one of these two | AudD API key |
| `SUPABASE_URL` | optional | Supabase project URL |
| `SUPABASE_KEY` | optional | Supabase anon/public key |
| `PORT` | set by Railway | Port to listen on (default: 8080) |

## Supabase Cache Setup

Run this once in your Supabase SQL editor:

```sql
create table tab_cache (
  cache_key  text primary key,
  tab_data   jsonb not null,
  created_at timestamptz default now()
);
```

## Disclaimer

This project is for educational purposes. Not affiliated with or endorsed by Ultimate-Guitar.com. Use responsibly.
