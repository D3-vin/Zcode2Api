package gateway

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var chatDumpMu sync.Mutex

func jsonTopKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			break
		}
		ks, ok := k.(string)
		if !ok {
			break
		}
		keys = append(keys, ks)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			break
		}
	}
	return keys
}

func dumpHdrNames(h http.Header) string {
	if h == nil {
		return ""
	}
	parts := make([]string, 0, len(h))
	for k, vs := range h {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "x-api-key" || lk == "cookie" {
			parts = append(parts, k+"=<redacted>")
			continue
		}
		for _, v := range vs {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, ", ")
}

// chatDumpPath is the dump file when Settings "chat dump" is on and
// CHAT_DUMP is set in env; empty means skip.
func (s *Server) chatDumpPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.chatDump || s.cfg.ChatDump == "" {
		return ""
	}
	return s.cfg.ChatDump
}

// dumpChatBodies appends the inbound client body and the origin payload
// (after normalizeBody) when CHAT_DUMP is set. Auth header values are redacted.
func dumpChatBodies(path string, r *http.Request, client, origin []byte, accMasked string) {
	if path == "" {
		return
	}
	var buf bytes.Buffer
	buf.WriteString("===== ")
	buf.WriteString(time.Now().Format(time.RFC3339))
	if r != nil {
		buf.WriteString(" ")
		buf.WriteString(r.Method)
		buf.WriteString(" ")
		buf.WriteString(r.URL.Path)
	}
	buf.WriteString(" acc=")
	buf.WriteString(accMasked)
	buf.WriteByte('\n')
	if r != nil {
		buf.WriteString("client-hdrs: ")
		buf.WriteString(dumpHdrNames(r.Header))
		buf.WriteByte('\n')
	}
	buf.WriteString("client-keys: ")
	buf.WriteString(strings.Join(jsonTopKeys(client), ","))
	buf.WriteString(" bytes=")
	buf.WriteString(strconv.Itoa(len(client)))
	buf.WriteByte('\n')
	buf.Write(client)
	if len(client) == 0 || client[len(client)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString("origin-keys: ")
	buf.WriteString(strings.Join(jsonTopKeys(origin), ","))
	buf.WriteString(" bytes=")
	buf.WriteString(strconv.Itoa(len(origin)))
	buf.WriteByte('\n')
	buf.Write(origin)
	if len(origin) == 0 || origin[len(origin)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')

	chatDumpMu.Lock()
	defer chatDumpMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("[chat-dump] %v", err)
		return
	}
	_, err = f.Write(buf.Bytes())
	_ = f.Close()
	if err != nil {
		log.Printf("[chat-dump] write: %v", err)
	}
}
