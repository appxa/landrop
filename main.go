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

func lanIP() string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
				continue
			}
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

// ---------- WebSocket ----------

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	id := shortID()
	c := colors[time.Now().UnixMilli()%int64(len(colors))]
	name := peerName(id)

	p := &peer{ID: id, Name: name, Color: c, ws: conn}

	pmu.Lock()
	peersByWS[conn] = p
	peersByID[id] = p
	pmu.Unlock()

	// send self info
	conn.WriteJSON(map[string]any{"type": "self", "id": id, "name": name, "color": c})

	// notify others
	broadcastExcept(conn, map[string]any{"type": "peer_joined", "id": id, "name": name, "color": c})
	broadcastPeerList()

	defer func() {
		pmu.Lock()
		delete(peersByWS, conn)
		delete(peersByID, id)
		pmu.Unlock()
		broadcastExcept(nil, map[string]any{"type": "peer_left", "id": id})
		broadcastPeerList()
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			conn.WriteJSON(map[string]any{"type": "error", "message": "Invalid JSON"})
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
				target.ws.WriteJSON(out)
			}
			// echo ack
			ack := map[string]any{"type": "message", "from": sender.ID, "fromName": sender.Name, "fromColor": sender.Color, "content": content, "timestamp": time.Now().UnixMilli(), "ack": true}
			conn.WriteJSON(ack)

		case "file":
			out := map[string]any{
				"type": "file", "from": sender.ID,
				"fromName": sender.Name, "fromColor": sender.Color,
				"fileId": msg["fileId"], "fileName": msg["fileName"],
				"fileSize": msg["fileSize"], "mimeType": msg["mimeType"],
				"timestamp": time.Now().UnixMilli(),
			}
			if target != nil {
				target.ws.WriteJSON(out)
			}
			ack := map[string]any{"type": "file", "from": sender.ID, "fromName": sender.Name, "fromColor": sender.Color, "fileId": msg["fileId"], "fileName": msg["fileName"], "fileSize": msg["fileSize"], "mimeType": msg["mimeType"], "timestamp": time.Now().UnixMilli(), "ack": true}
			conn.WriteJSON(ack)
		}
	}
}

func broadcastExcept(exclude *websocket.Conn, msg map[string]any) {
	pmu.Lock()
	defer pmu.Unlock()
	for conn, p := range peersByWS {
		if conn != exclude {
			if err := conn.WriteJSON(msg); err != nil {
				delete(peersByWS, conn)
				delete(peersByID, p.ID)
			}
		}
	}
}

func broadcastPeerList() {
	pmu.Lock()
	list := make([]map[string]any, 0, len(peersByWS))
	for _, p := range peersByWS {
		list = append(list, map[string]any{"id": p.ID, "name": p.Name, "color": p.Color})
	}
	pmu.Unlock()
	msg := map[string]any{"type": "peer_list", "peers": list}
	pmu.Lock()
	for conn, p := range peersByWS {
		if err := conn.WriteJSON(msg); err != nil {
			delete(peersByWS, conn)
			delete(peersByID, p.ID)
		}
	}
	pmu.Unlock()
}

// ---------- HTTP handlers ----------

func apiIP(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"ip": lanIP(), "port": port})
}

func upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file", 400)
		return
	}
	defer file.Close()
	buf, _ := io.ReadAll(file)
	id := fileID()
	mu.Lock()
	files[id] = &storedFile{Name: header.Filename, Size: int64(len(buf)), Mime: header.Header.Get("Content-Type"), Buffer: buf}
	mu.Unlock()
	// cleanup after 10min
	time.AfterFunc(10*time.Minute, func() { mu.Lock(); delete(files, id); mu.Unlock() })
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
