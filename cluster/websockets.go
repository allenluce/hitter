package cluster

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WebSockets struct {
	listeners []*websocket.Conn
	sockLock  sync.Mutex
}

var lastdata time.Time

func (ws *WebSockets) WriteJSON(message map[string]interface{}) {
	ws.sockLock.Lock()
	defer ws.sockLock.Unlock()
	if time.Now().Sub(lastdata) < time.Millisecond*10 {
		time.Sleep(time.Millisecond * 10)
	}
	lastdata = time.Now()
	for _, s := range ws.listeners {
		if err := s.WriteJSON(message); err != nil {
			if !strings.Contains(err.Error(), "broken pipe") &&
				!strings.Contains(err.Error(), "websocket: close sent") {
				Log("%s", err)
			}
		}
	}
}

func (ws *WebSockets) listen(socket *websocket.Conn) {
	for {
		_, message, err := socket.ReadMessage()
		if err != nil {
			break
		}
		Clus.SendEngine(string(message))
	}
}

func (ws *WebSockets) ServeWs(w http.ResponseWriter, r *http.Request) {
	var upgrader = websocket.Upgrader{}
	socket, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("upgrade:", err)
		return
	}
	ws.sockLock.Lock()
	ws.listeners = append(ws.listeners, socket)
	ws.sockLock.Unlock()
	// TODO: Attach listener here
	go ws.listen(socket)
}

var WS = WebSockets{}
