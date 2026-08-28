# lanDrop

LAN file & message sharing. Telegram-style UI. Zero config.

## Install & Run

```bash
git clone https://github.com/appxa/landrop.git
cd landrop
npm install
node server.js
```

Open `http://<your-ip>:3000` on any device on the same WiFi.

## How it works

Node.js server with WebSocket relay. No signup, no accounts, no database. Files are kept in memory for 10 minutes.

## Build installers

Desktop (Windows exe/msi, Linux deb/AppImage) via Electron:

```bash
npm install
npm run dist:linux   # deb + AppImage (run on Linux)
npm run dist:win     # exe + msi (run on Windows, or use CI)
```

Output goes to `dist/`.

Android APK — the real Node server runs on-device via nodejs-mobile (Capacitor plugin `@capawesome/capacitor-nodejs`):

```bash
npm install
npm run android:build   # needs Android SDK + JDK 17
# or use CI (GitHub Actions builds everything on push/tag)
```

The Android app bundles `server.js` into `public/nodejs/` (synced by `npm run android:sync`); the WebView UI connects to the on-device server at `127.0.0.1:3000`. Note the frontend auto-detects this — in a normal browser it uses relative URLs as before.

CI: push a tag (`v*`) or run the workflow manually — artifacts land in the Actions run (linux deb/AppImage, windows exe/msi, android apk).