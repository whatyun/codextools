package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type mobileRelayHostConfig struct {
	RelayURL string
	Room     string
	Token    string
	Key      string
}

type mobileRelayHostRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type mobileRelayHTTPResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type mobileRelayWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type mobileAppServerRuntime struct {
	port   uint16
	source string
	cmd    *exec.Cmd
}

var mobileAppServerState = struct {
	sync.Mutex
	runtime *mobileAppServerRuntime
}{}

func mobileRelayHostConfigFromSettings(settings backendSettings) *mobileRelayHostConfig {
	if !settings.MobileControlEnabled && strings.TrimSpace(getenv("CODEX_PLUS_MOBILE_RELAY_URL")) == "" {
		return nil
	}
	relayURL := envOrSetting("CODEX_PLUS_MOBILE_RELAY_URL", settings.MobileControlRelayURL)
	room := envOrSetting("CODEX_PLUS_MOBILE_RELAY_ROOM", settings.MobileControlRoom)
	token := strings.TrimSpace(getenv("CODEX_PLUS_MOBILE_RELAY_TOKEN"))
	if token == "" {
		token = room
	}
	key := envOrSetting("CODEX_PLUS_MOBILE_RELAY_KEY", settings.MobileControlKey)
	if relayURL == "" || room == "" || key == "" {
		return nil
	}
	return &mobileRelayHostConfig{RelayURL: relayURL, Room: room, Token: token, Key: key}
}

func envOrSetting(envName, setting string) string {
	if value := strings.TrimSpace(getenv(envName)); value != "" {
		return value
	}
	return strings.TrimSpace(setting)
}

func mobileRelayHostURL(config mobileRelayHostConfig) string {
	base := strings.TrimRight(strings.TrimSpace(config.RelayURL), "/")
	if base == "" {
		base = defaultMobileRelayURL
	}
	if !strings.HasSuffix(base, "/host") && !strings.Contains(base, "/host?") {
		base += "/host"
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + "room=" + url.QueryEscape(config.Room) + "&token=" + url.QueryEscape(config.Token)
}

func (r *launcherRuntime) startMobileRelayHost(helperPort uint16, config *mobileRelayHostConfig) {
	if config == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &mobileRelayHostRuntime{cancel: cancel, done: make(chan struct{})}
	r.mobileHost = runtime
	go func() {
		defer close(runtime.done)
		runMobileRelayHost(ctx, helperPort, *config)
	}()
	appendDiagnosticLog("mobile_relay.host_starting", map[string]any{
		"helper_port": helperPort,
		"relay_url":   config.RelayURL,
		"room":        config.Room,
		"host_url":    mobileRelayHostURL(*config),
	})
}

func (r *launcherRuntime) shutdownMobileRelayHost() {
	if r.mobileHost == nil {
		return
	}
	r.mobileHost.cancel()
	select {
	case <-r.mobileHost.done:
	case <-time.After(2 * time.Second):
	}
	shutdownManagedMobileAppServerRuntime()
	appendDiagnosticLog("mobile_relay.host_shutdown", map[string]any{})
}

func runMobileRelayHost(ctx context.Context, helperPort uint16, config mobileRelayHostConfig) {
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := runMobileRelayHostOnce(ctx, helperPort, config)
		if ctx.Err() != nil {
			return
		}
		appendDiagnosticLog("mobile_relay.host_disconnected", map[string]any{
			"helper_port": helperPort,
			"relay_url":   config.RelayURL,
			"room":        config.Room,
			"message":     errorString(err),
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func runMobileRelayHostOnce(ctx context.Context, helperPort uint16, config mobileRelayHostConfig) error {
	hostURL := mobileRelayHostURL(config)
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, hostURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect mobile relay host %s: %w", hostURL, err)
	}
	defer conn.Close()
	appendDiagnosticLog("mobile_relay.host_connected", map[string]any{
		"helper_port": helperPort,
		"relay_url":   config.RelayURL,
		"room":        config.Room,
	})
	relayWriter := &mobileRelayWriter{conn: conn}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	cipherBlock := mobileRelayCipher(config.Key)
	sessions := map[string]*mobileRelayAppServerSession{}
	defer func() {
		for _, session := range sessions {
			session.close()
		}
	}()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		response, err := handleMobileRelayHostMessage(ctx, helperPort, cipherBlock, data, sessions, relayWriter)
		if err != nil {
			appendDiagnosticLog("mobile_relay.message_failed", map[string]any{"error": err.Error()})
			continue
		}
		if response == nil {
			continue
		}
		if err := relayWriter.writeJSON(response); err != nil {
			return fmt.Errorf("failed to send mobile relay response: %w", err)
		}
	}
}

func (w *mobileRelayWriter) writeJSON(value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(value)
}

func handleMobileRelayHostMessage(ctx context.Context, helperPort uint16, block cipher.AEAD, data []byte, sessions map[string]*mobileRelayAppServerSession, relayWriter *mobileRelayWriter) (map[string]any, error) {
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	plaintextMode := stringFromAny(envelope["type"]) == "plaintext"
	request, err := decryptMobileRelayRequest(block, envelope)
	if err != nil {
		return nil, err
	}
	switch stringFromAny(request["type"]) {
	case "httpRequest":
		id := request["id"]
		response, err := proxyMobileRelayHTTPRequest(ctx, helperPort, request)
		if err != nil {
			response = mobileRelayHTTPResponse{
				Status:  http.StatusBadGateway,
				Headers: map[string]string{"content-type": "application/json; charset=utf-8"},
				Body:    `{"status":"failed","message":` + strconv.Quote(err.Error()) + `}`,
			}
		}
		return encodeMobileRelayPayload(block, plaintextMode, map[string]any{
			"type":    "httpResponse",
			"id":      id,
			"status":  response.Status,
			"headers": response.Headers,
			"body":    response.Body,
		})
	case "appServerConnect":
		return handleMobileRelayAppServerConnect(ctx, block, plaintextMode, request, sessions, relayWriter)
	case "appServerMessage":
		sessionID := stringFromAny(request["sessionId"])
		message := stringFromAny(request["message"])
		if session := sessions[sessionID]; session != nil && message != "" {
			return nil, session.send(message)
		}
	case "appServerClose":
		sessionID := stringFromAny(request["sessionId"])
		if session := sessions[sessionID]; session != nil {
			session.close()
			delete(sessions, sessionID)
		}
	}
	return nil, nil
}

func proxyMobileRelayHTTPRequest(ctx context.Context, helperPort uint16, request map[string]any) (mobileRelayHTTPResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(stringFromAny(request["method"])))
	if method == "" {
		method = http.MethodGet
	}
	path := stringFromAny(request["path"])
	if !strings.HasPrefix(path, "/") {
		path = "/"
	}
	body := stringFromAny(request["body"])
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, fmt.Sprintf("http://127.0.0.1:%d%s", helperPort, path), strings.NewReader(body))
	if err != nil {
		return mobileRelayHTTPResponse{}, err
	}
	contentType := mobileRelayContentType(request)
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	req.Header.Set("content-type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return mobileRelayHTTPResponse{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return mobileRelayHTTPResponse{}, err
	}
	headers := map[string]string{}
	for name, values := range resp.Header {
		if len(values) > 0 {
			headers[strings.ToLower(name)] = values[0]
		}
	}
	return mobileRelayHTTPResponse{Status: resp.StatusCode, Headers: headers, Body: string(respBody)}, nil
}

func mobileRelayContentType(request map[string]any) string {
	headers, _ := request["headers"].(map[string]any)
	for key, value := range headers {
		if strings.EqualFold(key, "content-type") {
			return strings.TrimSpace(stringFromAny(value))
		}
	}
	return ""
}

type mobileRelayAppServerSession struct {
	id     string
	conn   *websocket.Conn
	block  cipher.AEAD
	plain  bool
	relay  *mobileRelayWriter
	sendMu sync.Mutex
	done   chan struct{}
}

func handleMobileRelayAppServerConnect(ctx context.Context, block cipher.AEAD, plaintextMode bool, request map[string]any, sessions map[string]*mobileRelayAppServerSession, relay *mobileRelayWriter) (map[string]any, error) {
	sessionID := strings.TrimSpace(stringFromAny(request["sessionId"]))
	if sessionID == "" {
		sessionID = fmt.Sprintf("mobile-%d", time.Now().UnixNano())
	}
	if previous := sessions[sessionID]; previous != nil {
		previous.close()
	}
	conn, err := connectMobileAppServer(ctx)
	if err != nil {
		return encodeMobileRelayPayload(block, plaintextMode, map[string]any{
			"type":      "appServerClosed",
			"sessionId": sessionID,
			"error":     err.Error(),
		})
	}
	session := &mobileRelayAppServerSession{
		id:    sessionID,
		conn:  conn,
		block: block,
		plain: plaintextMode,
		relay: relay,
		done:  make(chan struct{}),
	}
	sessions[sessionID] = session
	go session.readLoop()
	return encodeMobileRelayPayload(block, plaintextMode, map[string]any{
		"type":      "appServerConnected",
		"id":        request["id"],
		"sessionId": sessionID,
	})
}

func (s *mobileRelayAppServerSession) send(message string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.conn.WriteMessage(websocket.TextMessage, []byte(message))
}

func (s *mobileRelayAppServerSession) readLoop() {
	defer close(s.done)
	for {
		messageType, data, err := s.conn.ReadMessage()
		if err != nil {
			s.writeRelay(map[string]any{"type": "appServerClosed", "sessionId": s.id, "error": err.Error()})
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		text := string(data)
		logMobileAppServerMessage(s.id, text)
		s.writeRelay(map[string]any{"type": "appServerMessage", "sessionId": s.id, "message": text})
	}
}

func (s *mobileRelayAppServerSession) writeRelay(payload map[string]any) {
	envelope, err := encodeMobileRelayPayload(s.block, s.plain, payload)
	if err != nil {
		appendDiagnosticLog("mobile_relay.app_server_encode_failed", map[string]any{"sessionId": s.id, "error": err.Error()})
		return
	}
	if err := s.relay.writeJSON(envelope); err != nil {
		appendDiagnosticLog("mobile_relay.app_server_send_failed", map[string]any{"sessionId": s.id, "error": err.Error()})
	}
}

func (s *mobileRelayAppServerSession) close() {
	_ = s.conn.Close()
	select {
	case <-s.done:
	case <-time.After(500 * time.Millisecond):
	}
}

func logMobileAppServerMessage(sessionID, text string) {
	var value map[string]any
	if json.Unmarshal([]byte(text), &value) != nil {
		return
	}
	appendDiagnosticLog("mobile_relay.app_server_message", map[string]any{
		"sessionId": sessionID,
		"id":        value["id"],
		"method":    stringFromAny(value["method"]),
		"hasError":  value["error"] != nil,
	})
}

func connectMobileAppServer(ctx context.Context) (*websocket.Conn, error) {
	runtime, err := ensureMobileAppServerRuntime(ctx)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, fmt.Sprintf("ws://127.0.0.1:%d/rpc", runtime.port), nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func ensureMobileAppServerRuntime(ctx context.Context) (*mobileAppServerRuntime, error) {
	mobileAppServerState.Lock()
	defer mobileAppServerState.Unlock()
	if runtime := mobileAppServerState.runtime; runtime != nil && mobileAppServerReady(runtime.port) {
		return runtime, nil
	}
	if runtime := existingMobileAppServerRuntime(); runtime != nil {
		mobileAppServerState.runtime = runtime
		return runtime, nil
	}
	runtime, err := startMobileAppServerRuntime(ctx)
	if err != nil {
		return nil, err
	}
	mobileAppServerState.runtime = runtime
	return runtime, nil
}

func existingMobileAppServerRuntime() *mobileAppServerRuntime {
	for _, key := range []string{"CODEX_PLUS_APP_SERVER_URL", "CODEX_APP_SERVER_URL"} {
		if port := mobileAppServerPortFromURL(os.Getenv(key)); port != 0 && mobileAppServerReady(port) {
			return &mobileAppServerRuntime{port: port, source: "external"}
		}
	}
	return nil
}

func startMobileAppServerRuntime(ctx context.Context) (*mobileAppServerRuntime, error) {
	port, err := reserveMobileAppServerPort()
	if err != nil {
		return nil, err
	}
	codex := codexCLIExecutable()
	if strings.TrimSpace(codex) == "" {
		return nil, errors.New("未找到 Codex CLI，无法启动手机控制 app-server")
	}
	cmd := exec.Command(codex, "app-server", "--listen", fmt.Sprintf("ws://127.0.0.1:%d", port))
	cmd.Env = append(os.Environ(), "CODEX_HOME="+codexHomeDir())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	hideSubprocessWindow(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("无法启动 ChatGPT Codex app-server：%w", err)
	}
	runtime := &mobileAppServerRuntime{port: port, source: "managed", cmd: cmd}
	if err := waitForMobileAppServerReady(ctx, port); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	go func() {
		_ = cmd.Wait()
		mobileAppServerState.Lock()
		if mobileAppServerState.runtime == runtime {
			mobileAppServerState.runtime = nil
		}
		mobileAppServerState.Unlock()
	}()
	return runtime, nil
}

func shutdownManagedMobileAppServerRuntime() {
	mobileAppServerState.Lock()
	runtime := mobileAppServerState.runtime
	if runtime == nil || runtime.cmd == nil || runtime.source != "managed" {
		mobileAppServerState.Unlock()
		return
	}
	mobileAppServerState.runtime = nil
	mobileAppServerState.Unlock()
	if runtime.cmd.Process != nil {
		_ = runtime.cmd.Process.Kill()
	}
}

func reserveMobileAppServerPort() (uint16, error) {
	for i := 0; i < 20; i++ {
		listener, err := netListen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := uint16(listener.Addr().(*net.TCPAddr).Port)
		_ = listener.Close()
		if port != localRelayProxyPort && port != 0 {
			return port, nil
		}
	}
	return 0, errors.New("无法为 ChatGPT Codex app-server 预留端口")
}

func waitForMobileAppServerReady(ctx context.Context, port uint16) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if mobileAppServerReady(port) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("ChatGPT Codex app-server 启动超时")
}

func mobileAppServerReady(port uint16) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/readyz", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func mobileAppServerPortFromURL(value string) uint16 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return 0
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "http" {
		return 0
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return 0
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return uint16(port)
}

func mobileRelayCipher(key string) cipher.AEAD {
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}

func encryptMobileRelayPayload(block cipher.AEAD, payload any) (map[string]any, error) {
	nonce := mobileRelayNonce()
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	encrypted := block.Seal(nil, nonce, plain, nil)
	return map[string]any{
		"type":    "encrypted",
		"nonce":   base64.RawURLEncoding.EncodeToString(nonce),
		"payload": base64.RawURLEncoding.EncodeToString(encrypted),
	}, nil
}

func encodeMobileRelayPayload(block cipher.AEAD, plaintextMode bool, payload map[string]any) (map[string]any, error) {
	if plaintextMode {
		return map[string]any{"type": "plaintext", "payload": payload}, nil
	}
	return encryptMobileRelayPayload(block, payload)
}

func decryptMobileRelayRequest(block cipher.AEAD, envelope map[string]any) (map[string]any, error) {
	if stringFromAny(envelope["type"]) == "plaintext" {
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			return nil, errors.New("手机控制明文数据包缺少 payload")
		}
		return payload, nil
	}
	return decryptMobileRelayEnvelope(block, envelope)
}

func decryptMobileRelayEnvelope(block cipher.AEAD, envelope map[string]any) (map[string]any, error) {
	if stringFromAny(envelope["type"]) != "encrypted" {
		return nil, errors.New("手机控制数据包未加密")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(stringFromAny(envelope["nonce"]))
	if err != nil {
		return nil, err
	}
	if len(nonce) != block.NonceSize() {
		return nil, errors.New("手机控制 nonce 长度无效")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(stringFromAny(envelope["payload"]))
	if err != nil {
		return nil, err
	}
	plain, err := block.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("手机控制数据解密失败")
	}
	var payload map[string]any
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func mobileRelayNonce() []byte {
	nonce := make([]byte, 12)
	binary.LittleEndian.PutUint64(nonce[:8], uint64(time.Now().UnixMilli()))
	if _, err := rand.Read(nonce[8:]); err != nil {
		copy(nonce[8:], []byte{1, 2, 3, 4})
	}
	return nonce
}

func mobileRelayShareURL(settings backendSettings) string {
	relayURL := strings.TrimSpace(settings.MobileControlRelayURL)
	if relayURL == "" {
		relayURL = defaultMobileRelayURL
	}
	httpURL := mobileRelayHTTPURL(relayURL)
	if httpURL == "" {
		return ""
	}
	values := url.Values{}
	if room := strings.TrimSpace(settings.MobileControlRoom); room != "" {
		values.Set("room", room)
	}
	if key := strings.TrimSpace(settings.MobileControlKey); key != "" {
		values.Set("key", key)
	}
	query := values.Encode()
	if query == "" {
		return strings.TrimRight(httpURL, "/") + "/mobile"
	}
	return strings.TrimRight(httpURL, "/") + "/mobile?" + query
}

func mobileRelayHTTPURL(relayURL string) string {
	trimmed := strings.TrimSpace(relayURL)
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(trimmed, "ws://"):
		return "http://" + strings.TrimPrefix(trimmed, "ws://")
	case strings.HasPrefix(trimmed, "wss://"):
		return "https://" + strings.TrimPrefix(trimmed, "wss://")
	case strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://"):
		return trimmed
	default:
		return ""
	}
}

var getenv = os.Getenv
var netListen = net.Listen
