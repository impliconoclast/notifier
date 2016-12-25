package main

import (
	"bytes"
	// "fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/atotto/clipboard"
	"github.com/gorilla/websocket"
)

type Notifier interface {
	Notifier() <-chan bool
}

type StringNotifier interface {
	Notifier
	String() string
}

type clipboardMonitor struct {
	cur string
	mux sync.Mutex
	c   chan bool
}

func newClipboardMonitor() *clipboardMonitor {
	if clipboard.Unsupported {
		panic("clipboard not supported on your system")
	}
	m := &clipboardMonitor{
		c: make(chan bool),
	}
	m.read()
	go m.do()
	return m
}

func (m *clipboardMonitor) Notifier() <-chan bool {
	m.mux.Lock()
	defer m.mux.Unlock()
	return m.c
}

func (m *clipboardMonitor) String() string {
	m.mux.Lock()
	defer m.mux.Unlock()
	return m.cur
}

func (m *clipboardMonitor) do() {
	// Worry(me): 1초마다 하면 되나?
	for range time.Tick(time.Second) {
		m.read()
	}
}

func (m *clipboardMonitor) read() {
	s, err := clipboard.ReadAll()
	if err != nil {
		log.Print("clipboard.ReadAll()", err)
		return
	}
	// Worry(me): len(s) 가 너무 크면?
	if m.cur == s {
		return
	}
	m.mux.Lock()
	close(m.c)
	m.cur = s
	m.c = make(chan bool)
	m.mux.Unlock()
}

// TODO(me): close 메서드 구현

func indexHandler(w http.ResponseWriter, req *http.Request) {
	const e = 1024
	buf := new(bytes.Buffer)
	buf.Grow(len(tpltext) + e)
	if err := tpl.Execute(buf, nil); err != nil {
		respondError(w, err)
		return
	}
	io.Copy(w, buf)
}

func wsHandler(notifier Notifier, w io.Writer) error {
	n := notifier.(StringNotifier)
	src := strings.NewReader(n.String())
	_, err := io.Copy(w, src)
	return err
}

type notifierFunc func(notifier Notifier, w io.Writer) error

type notifierHandler struct {
	notifier Notifier
	fn       notifierFunc
}

func newNotifierHandler(notifier Notifier, fn notifierFunc) http.Handler {
	return &notifierHandler{notifier, fn}
}

func (n *notifierHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	upgrader := &websocket.Upgrader{
		EnableCompression: true,
	}
	con, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		respondError(w, err)
		return
	}
	c := n.notifier.Notifier()
	err = n.wake(con)
	for err == nil {
		<-c
		c = n.notifier.Notifier()
		err = n.wake(con)
	}
	log.Print(err)
}

func (n *notifierHandler) wake(con *websocket.Conn) error {
	w, err := con.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	if err := n.fn(n.notifier, w); err != nil {
		return err
	}
	return w.Close()
}

func respondError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func main() {
	m := newClipboardMonitor()
	http.HandleFunc("/", indexHandler)
	http.Handle("/ws", newNotifierHandler(m, wsHandler))
	log.Fatal(http.ListenAndServe(":12345", nil))
}

const tpltext = `<!DOCTYPE html>
<html>
<head>
	<meta chaarset="utf-8" />
	<title>클립보드 테스트</title>
</head>
<body>
<div id="clipboard"></div>
<script>
var div = document.getElementById("clipboard");
var ws = new WebSocket("ws://localhost:12345/ws");

ws.onopen = function() {};

ws.onmessage = function(message) {
	div.innerHTML = message.data;
};

ws.onclose = function() {
	alert("connection closed");
};

</script>
</body>
</html>
`

var tpl = template.Must(template.New("master").Parse(tpltext))
