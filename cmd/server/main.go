package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

type Client struct {
	conn     *websocket.Conn
	// send — очередь исходящих сообщений клиенту.
	// Отдельная горутина writePump читает из этого канала и пишет в WebSocket.
	send     chan Message
	room     *Room
	nickname string
	token    string
}

type Room struct {
	name       string
	clients    map[*Client]bool
	// broadcast — входящие в комнату сообщения, которые нужно разослать всем.
	broadcast chan Message
	// register/unregister — события подключения/отключения клиентов.
	// Идея: все изменения списка клиентов идут через один цикл (run),
	// чтобы не размазывать синхронизацию по коду.
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
}

type ChatServer struct {
	mu       sync.Mutex
	rooms    map[string]*Room
	// messages хранит историю сообщений по комнатам (in-memory).
	messages map[string][]Message // room -> history
	// tokens — "примитивная авторизация": токен -> nickname (in-memory).
	// Это не JWT и не безопасное решение, но для учебного проекта достаточно.
	tokens map[string]string // token -> nickname
}

var (
	chat = ChatServer{
		rooms:    make(map[string]*Room),
		messages: make(map[string][]Message),
		tokens:   make(map[string]string),
	}

	upgrader = websocket.Upgrader{
		// CheckOrigin отключает проверку Origin.
		// Так удобнее тестировать локально из любых клиентов/страниц,
		// но для продакшена это было бы небезопасно.
		CheckOrigin: func(r *http.Request) bool { return true }, // dev-only
	}
)

func newRoom(name string) *Room {
	// newRoom создаёт комнату и запускает её event-loop в отдельной горутине.
	room := &Room{
		name:       name,
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
	go room.run()
	return room
}

func (r *Room) run() {
	// run — основной цикл комнаты: обрабатывает подключение/отключение и рассылку сообщений.
	// Центральный event-loop комнаты. Он принимает события из каналов и:
	// - добавляет/убирает клиентов
	// - при подключении отдаёт историю (последние 10 сообщений)
	// - рассылает новые сообщения всем подключённым клиентам
	for {
		select {
		case client := <-r.register:
			r.mu.Lock()
			r.clients[client] = true
			r.mu.Unlock()

			chat.mu.Lock()
			history := chat.messages[r.name]
			chat.mu.Unlock()

			start := 0
			if len(history) > 10 {
				start = len(history) - 10
			}
			// История отправляется только подключившемуся клиенту.
			for _, msg := range history[start:] {
				client.send <- msg
			}

		case client := <-r.unregister:
			r.mu.Lock()
			if _, ok := r.clients[client]; ok {
				delete(r.clients, client)
				close(client.send)
			}
			r.mu.Unlock()

		case msg := <-r.broadcast:
			r.mu.Lock()
			for client := range r.clients {
				select {
				case client.send <- msg:
				default:
					// Если клиент "не успевает" читать (очередь переполнилась),
					// считаем его медленным/зависшим и отключаем, чтобы не стопорить рассылку.
					close(client.send)
					delete(r.clients, client)
				}
			}
			r.mu.Unlock()
		}
	}
}

func authHandler(w http.ResponseWriter, r *http.Request) {
	// authHandler выдаёт токен по имени пользователя (POST /auth).
	// Простейший эндпоинт авторизации:
	// принимает имя и возвращает токен, который потом используется при подключении к WebSocket.
	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "invalid request, need {\"name\":\"...\"}", http.StatusBadRequest)
		return
	}

	// Токен здесь не криптографический, просто уникальная строка для демо.
	token := fmt.Sprintf("%d-%s", time.Now().UnixNano(), req.Name)
	chat.mu.Lock()
	chat.tokens[token] = req.Name
	chat.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// wsHandler апгрейдит HTTP до WebSocket и подключает клиента к указанной комнате (GET /ws).
	// WebSocket endpoint. Ожидает query-параметры:
	// - token: выданный /auth
	// - room: имя комнаты
	token := r.URL.Query().Get("token")
	roomName := r.URL.Query().Get("room")
	if token == "" || roomName == "" {
		http.Error(w, "missing token or room", http.StatusBadRequest)
		return
	}

	chat.mu.Lock()
	nickname, ok := chat.tokens[token]
	chat.mu.Unlock()
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Upgrade переключает HTTP соединение на WebSocket протокол.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}
	defer conn.Close()

	chat.mu.Lock()
	room, exists := chat.rooms[roomName]
	if !exists {
		room = newRoom(roomName)
		chat.rooms[roomName] = room
	}
	chat.mu.Unlock()

	client := &Client{
		conn:     conn,
		send:     make(chan Message, 256),
		room:     room,
		nickname: nickname,
		token:    token,
	}

	room.register <- client

	// Пишем в сокет и читаем из сокета раздельно:
	// - writePump: только отправка (берёт из client.send)
	// - readPump: только чтение входящих сообщений клиента
	go client.writePump()
	client.readPump()
}

func (c *Client) writePump() {
	// writePump отправляет сообщения клиенту и делает ping, чтобы держать соединение “живым”.
	// writePump отправляет клиенту сообщения из c.send и поддерживает соединение ping'ами.
	ticker := time.NewTicker(60 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump() {
	// readPump читает сообщения клиента и транслирует их в комнату.
	// readPump читает сообщения клиента из WebSocket.
	// При выходе из функции клиент отписывается из комнаты.
	defer func() {
		c.room.unregister <- c
		_ = c.conn.Close()
	}()

	// Ограничиваем максимальный размер входящего сообщения.
	c.conn.SetReadLimit(512)
	// Таймаут чтения + PongHandler: если клиент отвечает на ping, продлеваем дедлайн.
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var incoming struct {
			Text string `json:"text"`
		}
		if err := c.conn.ReadJSON(&incoming); err != nil {
			break
		}
		if incoming.Text == "" {
			continue
		}

		// Сервер добавляет метаданные (ник и время) перед рассылкой.
		msg := Message{
			User:      c.nickname,
			Text:      incoming.Text,
			Timestamp: time.Now().Format("15:04:05"),
		}

		// Сохраняем историю в памяти.
		chat.mu.Lock()
		chat.messages[c.room.name] = append(chat.messages[c.room.name], msg)
		chat.mu.Unlock()

		c.room.broadcast <- msg
	}
}

func main() {
	// main регистрирует HTTP-роуты и запускает сервер.
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	http.HandleFunc("/auth", authHandler)
	http.HandleFunc("/ws", wsHandler)

	fmt.Println("Server listening on", *addr)
	// nil => используем http.DefaultServeMux, куда добавили HandleFunc выше.
	log.Fatal(http.ListenAndServe(*addr, nil))
}
