package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed public
var webFS embed.FS

// ---------- in-memory file store ----------

type storedFile struct {
	Name   string
	Size   int64
	Mime   string
	Buffer []byte
}

var (
	mu     sync.Mutex
	files  = map[string]*storedFile{}
	port   = 3000
	colors = []string{
		"#e17076", "#f7a26b", "#f6c25a", "#5fc86e",
		"#6bc7e0", "#6d9cf0", "#9b85e0", "#d06eab",
	}
)

// ---------- peer store ----------

type peer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	ws    *websocket.Conn
	// writeMu serialises writes. gorilla/websocket allows only one concurrent
	// writer per connection, and a peer can be written to from the read loop,
	// from broadcastPeerList and from a direct relay at the same time.
	writeMu sync.Mutex
}

// send writes one JSON message safely. A failed write means the peer is gone.
func (p *peer) send(msg map[string]any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.ws.WriteJSON(msg)
}

var (
	pmu       sync.Mutex
	peersByWS = map[*websocket.Conn]*peer{}
	peersByID = map[string]*peer{}
)

// ---------- helpers ----------

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func shortID() string { return randHex(4) }  // 8 hex chars
func fileID() string  { return randHex(16) } // 32 hex chars

func peerName(id string) string { return "User_" + id[:4] }

// Virtual/tunnel interfaces must never be advertised as the join address:
// a phone on the WiFi cannot reach them.
var virtualIfaces = map[string]bool{
	"docker": true, "veth": true, "br-": true, "virbr": true, "vmnet": true,
	"tun": true, "tap": true, "wg": true, "zt": true, "utun": true, "tailscale": true,
}

func isVirtualIface(name string) bool {
	if virtualIfaces[name] {
		return true
	}
	for prefix := range virtualIfaces {
		if len(prefix) >= 3 && len(name) >= 3 && name[:3] == prefix {
			return true
		}
	}
	return false
}

// lanIPs returns every IPv4 address other devices could plausibly reach,
// best candidate first. Tunnels and virtual adapters are excluded.
func lanIPs() []string {
	interfaces, _ := net.Interfaces()
	var out []string
	seen := map[string]bool{}

	add := func(ip net.IP) {
		v4 := ip.To4()
		if v4 == nil {
			return // IPv4 only: link-local IPv6 is useless to a phone
		}
		s := v4.String()
		if s == "127.0.0.1" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	// Preferred: the address the OS itself would use to reach the outside
	// world. This is the interface other devices on the same WiFi can reach.
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			add(ua.IP)
		}
		conn.Close()
	}

	// Fallbacks / extras: real hardware interfaces, wired before wireless.
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Point-to-point links are tunnels (VPN/wireguard/tun). A phone on the
		// WiFi can never reach them, so never advertise one.
		if iface.Flags&net.FlagPointToPoint != 0 || isVirtualIface(iface.Name) {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				add(ipnet.IP)
			}
		}
	}
	return out
}

func lanIP() string {
	if ips := lanIPs(); len(ips) > 0 {
		return ips[0]
	}
	return "127.0.0.1"
}

// ---------- WebSocket ----------

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// Keepalive. Without a read deadline a phone that sleeps, changes WiFi or gets
// backgrounded leaves its peer registered forever, so it keeps showing up in
// the contact list -- and anything sent to that ghost peer is silently lost.
const (
	pongWait   = 60 * time.Second
	pingPeriod = 25 * time.Second
)

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	id := shortID()
	c := colors[time.Now().UnixMilli()%int64(len(colors))]
	name := peerName(id)

	p := &peer{ID: id, Name: name, Color: c, ws: conn}

	pmu.Lock()
	peersByWS[conn] = p
	peersByID[id] = p
	pmu.Unlock()

	// ping ticker; a peer that stops answering is dropped by the read deadline
	stopPing := make(chan struct{})
	go func() {
		t := time.NewTicker(pingPeriod)
		defer t.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-t.C:
				p.writeMu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				p.writeMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	// send self info
	p.send(map[string]any{"type": "self", "id": id, "name": name, "color": c})

	// notify others
	broadcastExcept(conn, map[string]any{"type": "peer_joined", "id": id, "name": name, "color": c})
	broadcastPeerList()

	defer func() {
		close(stopPing)
		pmu.Lock()
		delete(peersByWS, conn)
		delete(peersByID, id)
		pmu.Unlock()
		conn.Close()
		broadcastExcept(nil, map[string]any{"type": "peer_left", "id": id})
		broadcastPeerList()
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		// Any traffic counts as liveness, not just pongs.
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			p.send(map[string]any{"type": "error", "message": "Invalid JSON"})
			continue
		}
		typ, _ := msg["type"].(string)
		toID, _ := msg["to"].(string)

		pmu.Lock()
		sender := peersByWS[conn]
		target := peersByID[toID]
		pmu.Unlock()
		if sender == nil {
			continue
		}

		switch typ {
		case "message":
			content, _ := msg["content"].(string)
			out := map[string]any{
				"type": "message", "from": sender.ID,
				"fromName": sender.Name, "fromColor": sender.Color,
				"content": content, "timestamp": time.Now().UnixMilli(),
			}
			if target != nil {
				target.send(out)
			}
			// echo ack
			ack := map[string]any{"type": "message", "from": sender.ID, "fromName": sender.Name, "fromColor": sender.Color, "content": content, "timestamp": time.Now().UnixMilli(), "ack": true}
			p.send(ack)

		case "file":
			out := map[string]any{
				"type": "file", "from": sender.ID,
				"fromName": sender.Name, "fromColor": sender.Color,
				"fileId": msg["fileId"], "fileName": msg["fileName"],
				"fileSize": msg["fileSize"], "mimeType": msg["mimeType"],
				"timestamp": time.Now().UnixMilli(),
			}
			if target != nil {
				target.send(out)
			}
			ack := map[string]any{"type": "file", "from": sender.ID, "fromName": sender.Name, "fromColor": sender.Color, "fileId": msg["fileId"], "fileName": msg["fileName"], "fileSize": msg["fileSize"], "mimeType": msg["mimeType"], "timestamp": time.Now().UnixMilli(), "ack": true}
			p.send(ack)
		}
	}
}

func broadcastExcept(exclude *websocket.Conn, msg map[string]any) {
	pmu.Lock()
	targets := make([]*peer, 0, len(peersByWS))
	for conn, p := range peersByWS {
		if conn != exclude {
			targets = append(targets, p)
		}
	}
	pmu.Unlock()
	for _, p := range targets {
		if err := p.send(msg); err != nil {
			pmu.Lock()
			delete(peersByWS, p.ws)
			delete(peersByID, p.ID)
			pmu.Unlock()
		}
	}
}

func broadcastPeerList() {
	pmu.Lock()
	list := make([]map[string]any, 0, len(peersByWS))
	targets := make([]*peer, 0, len(peersByWS))
	for _, p := range peersByWS {
		list = append(list, map[string]any{"id": p.ID, "name": p.Name, "color": p.Color})
		targets = append(targets, p)
	}
	pmu.Unlock()
	msg := map[string]any{"type": "peer_list", "peers": list}
	for _, p := range targets {
		if err := p.send(msg); err != nil {
			pmu.Lock()
			delete(peersByWS, p.ws)
			delete(peersByID, p.ID)
			pmu.Unlock()
		}
	}
}

// ---------- HTTP handlers ----------

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func apiIP(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	ips := lanIPs()
	if len(ips) == 0 {
		ips = []string{"127.0.0.1"}
	}
	json.NewEncoder(w).Encode(map[string]any{
		"ip":   ips[0],
		"port": port,
		"ips":  ips,
	})
}

// maxUpload caps a single upload. Files are buffered in RAM, so an unbounded
// read lets one large video from a phone exhaust the server's memory.
const maxUpload = 2 << 30 // 2 GiB

func upload(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	if r.ContentLength > maxUpload {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Upload failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	buf, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Upload failed while reading file", http.StatusBadRequest)
		return
	}
	id := fileID()
	mu.Lock()
	files[id] = &storedFile{Name: header.Filename, Size: int64(len(buf)), Mime: header.Header.Get("Content-Type"), Buffer: buf}
	mu.Unlock()
	// cleanup after 10min
	time.AfterFunc(10*time.Minute, func() { mu.Lock(); delete(files, id); mu.Unlock() })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"fileId": id})
}

func download(w http.ResponseWriter, r *http.Request) {
	// parse /download/{fileId}
	prefix := "/download/"
	id := ""
	if len(r.URL.Path) > len(prefix) {
		id = r.URL.Path[len(prefix):]
	}
	mu.Lock()
	f, ok := files[id]
	mu.Unlock()
	if !ok {
		http.Error(w, "Not found", 404)
		return
	}
	w.Header().Set("Content-Type", f.Mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, f.Name))
	w.Write(f.Buffer)
}

// ---------- open browser ----------

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	exec.Command(cmd, args...).Start()
}

// ---------- main ----------

func main() {
	// try port, increment if busy
	for {
		ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
		if err == nil {
			ln.Close()
			break
		}
		port++
	}

	// API
	http.HandleFunc("/api/ip", apiIP)
	http.HandleFunc("/upload", upload)
	http.HandleFunc("/download/", download)

	// Static files from embedded public/
	staticFS := http.FileServer(http.FS(webFS))

	// Root: WS upgrade or static
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if websocket.IsWebSocketUpgrade(r) {
			handleWS(w, r)
			return
		}
		if r.URL.Path == "/" {
			data, err := webFS.ReadFile("public/index.html")
			if err != nil {
				http.Error(w, "Not found", 404)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}
		staticFS.ServeHTTP(w, r)
	})

	ip := lanIP()
	url := fmt.Sprintf("http://%s:%d", ip, port)
	log.Printf("lanDrop running on %s", url)
	openBrowser(url)

	log.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), nil))
}
