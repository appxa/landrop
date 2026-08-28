# lanDrop

LAN file & message sharing. Telegram-style UI. Zero config. Single ~8MB Go binary.

## Run

```bash
go run .          # or: ./dist/landrop
```

The app starts a server on your LAN, prints the URL, and opens your browser.
Open `http://<your-ip>:3000` on any device on the same WiFi to join.

## Build

```bash
sh scripts/build-go.sh
```

Outputs to `dist/`:
- `dist/landrop` — Linux amd64 binary (~8MB)
- `dist/landrop.exe` — Windows amd64 binary (~8MB)
- `dist/landrop_1.0.0_amd64.deb` — Debian/Ubuntu package (~2.3MB)

Cross-compiles from any machine; no Node.js or Electron needed. The whole app
(server + UI) is a single embedded binary.

## How it works

Go server with WebSocket relay. No signup, no accounts, no database. Files
are kept in memory for 10 minutes. UI is `public/index.html`, embedded into
the binary at compile time.

## CI

`.github/workflows/build.yml` builds linux deb + windows exe on push/tag —
artifacts land in the Actions run.
