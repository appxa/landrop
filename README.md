# localchat

LAN file & message sharing. Telegram-style UI. Zero config.

## Install & Run

```bash
git clone https://github.com/appxa/localchat.git
cd localchat
npm install
node server.js
```

Open `http://<your-ip>:3000` on any device on the same WiFi.

## How it works

Node.js server with WebSocket relay. No signup, no accounts, no database. Files are kept in memory for 10 minutes.