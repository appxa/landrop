const express = require('express');
const http = require('http');
const { WebSocketServer } = require('ws');
const { v4: uuidv4 } = require('uuid');
const path = require('path');
const fs = require('fs');
const multer = require('multer');

const PORT = process.env.PORT || 3000;

const app = express();
const server = http.createServer(app);
const wss = new WebSocketServer({ server });

// --- In-memory stores ---
const peers = new Map();          // ws -> { id, name, color }
const peerInfo = new Map();       // id -> { ws, name, color }
const files = new Map();          // fileId -> { name, size, mime, buffer }

// Multer for file uploads (memoryStorage)
const upload = multer({ storage: multer.memoryStorage() });

// --- Serve static files ---
app.use(express.static(path.join(__dirname, 'public')));

// --- File upload endpoint ---
app.post('/upload', upload.single('file'), (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'No file' });
  const fileId = uuidv4();
  files.set(fileId, {
    name: req.file.originalname,
    size: req.file.size,
    mime: req.file.mimetype,
    buffer: req.file.buffer,
  });
  // Auto-cleanup after 10 minutes
  setTimeout(() => files.delete(fileId), 10 * 60 * 1000);
  res.json({ fileId });
});

// --- File download endpoint ---
app.get('/download/:fileId', (req, res) => {
  const f = files.get(req.params.fileId);
  if (!f) return res.status(404).json({ error: 'File not found or expired' });
  res.setHeader('Content-Type', f.mime);
  res.setHeader('Content-Disposition', `attachment; filename="${f.name}"`);
  res.send(f.buffer);
});

// --- WebSocket ---
wss.on('connection', (ws) => {
  const id = uuidv4().slice(0, 8);
  const colors = ['#e17076','#f7a26b','#f6c25a','#5fc86e','#6bc7e0','#6d9cf0','#9b85e0','#d06eab'];
  const color = colors[Math.floor(Math.random() * colors.length)];
  const name = `User_${id.slice(0, 4)}`;

  peers.set(ws, { id, name, color });
  peerInfo.set(id, { ws, name, color });

  // Send own info
  ws.send(JSON.stringify({ type: 'self', id, name, color }));

  // Notify all of new peer, send full list
  broadcast({ type: 'peer_joined', id, name, color }, ws);
  broadcastPeerList();

  console.log(`[+] ${name} (${id}) — ${wss.clients.size} connected`);

  // --- Incoming messages ---
  ws.on('message', (data) => {
    let msg;
    try {
      msg = JSON.parse(data);
    } catch {
      return ws.send(JSON.stringify({ type: 'error', message: 'Invalid JSON' }));
    }

    const sender = peers.get(ws);
    if (!sender) return;

    if (msg.type === 'message' && msg.to) {
      const target = peerInfo.get(msg.to);
      if (target && target.ws.readyState === 1) {
        target.ws.send(JSON.stringify({
          type: 'message',
          from: sender.id,
          fromName: sender.name,
          fromColor: sender.color,
          content: msg.content,
          timestamp: Date.now(),
        }));
        // Echo back to sender for confirmation
        ws.send(JSON.stringify({
          type: 'message',
          from: sender.id,
          fromName: sender.name,
          fromColor: sender.color,
          content: msg.content,
          timestamp: Date.now(),
          ack: true,
        }));
      } else {
        ws.send(JSON.stringify({ type: 'error', message: 'Recipient disconnected' }));
      }
    }

    if (msg.type === 'file' && msg.to) {
      const target = peerInfo.get(msg.to);
      if (target && target.ws.readyState === 1) {
        target.ws.send(JSON.stringify({
          type: 'file',
          from: sender.id,
          fromName: sender.name,
          fromColor: sender.color,
          fileId: msg.fileId,
          fileName: msg.fileName,
          fileSize: msg.fileSize,
          mimeType: msg.mimeType,
          timestamp: Date.now(),
        }));
        ws.send(JSON.stringify({
          type: 'file',
          from: sender.id,
          fromName: sender.name,
          fromColor: sender.color,
          fileId: msg.fileId,
          fileName: msg.fileName,
          fileSize: msg.fileSize,
          mimeType: msg.mimeType,
          timestamp: Date.now(),
          ack: true,
        }));
      } else {
        ws.send(JSON.stringify({ type: 'error', message: 'Recipient disconnected' }));
      }
    }
  });

  // --- Disconnect ---
  ws.on('close', () => {
    const p = peers.get(ws);
    if (p) {
      peerInfo.delete(p.id);
      peers.delete(ws);
      broadcast({ type: 'peer_left', id: p.id });
      broadcastPeerList();
      console.log(`[-] ${p.name} (${p.id}) — ${wss.clients.size} connected`);
    }
  });
});

function broadcast(data, exclude) {
  const msg = JSON.stringify(data);
  wss.clients.forEach((client) => {
    if (client !== exclude && client.readyState === 1) {
      client.send(msg);
    }
  });
}

function broadcastPeerList() {
  const list = [];
  peers.forEach((p) => list.push({ id: p.id, name: p.name, color: p.color }));
  const msg = JSON.stringify({ type: 'peer_list', peers: list });
  wss.clients.forEach((client) => {
    if (client.readyState === 1) client.send(msg);
  });
}

server.listen(PORT, '0.0.0.0', () => {
  console.log(`Snapdrop-clone running on http://0.0.0.0:${PORT}`);
});
