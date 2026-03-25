package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

type Message struct {
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

func main() {
	// main подключается к серверу и запускает интерактивный CLI-чат.
	// CLI-параметры клиента:
	// -name обязателен (используется при получении токена)
	// -room имя комнаты
	// -host адрес сервера (host:port)
	name := flag.String("name", "", "your name (required)")
	room := flag.String("room", "general", "room name")
	host := flag.String("host", "localhost:8080", "server host:port")
	flag.Parse()

	if strings.TrimSpace(*name) == "" {
		fmt.Println("Usage: go run ./cmd/client -name Alice [-room general] [-host localhost:8080]")
		os.Exit(1)
	}

	// 1) Получаем токен по HTTP.
	token, err := getToken(*host, *name)
	if err != nil {
		log.Fatal(err)
	}

	// 2) Формируем WebSocket URL с query token+room.
	u := url.URL{
		Scheme:   "ws",
		Host:     *host,
		Path:     "/ws",
		RawQuery: "token=" + url.QueryEscape(token) + "&room=" + url.QueryEscape(*room),
	}

	// 3) Подключаемся по WebSocket.
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Printf("Connected to room %q as %q\n", *room, *name)

	// Чтение входящих сообщений делаем в отдельной горутине,
	// чтобы основной поток мог одновременно читать stdin и отправлять сообщения.
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				log.Println("read error:", err)
				return
			}
			fmt.Printf("\r[%s] %s: %s\n", msg.Timestamp, msg.User, msg.Text)
			fmt.Print("> ")
		}
	}()

	// Основной цикл: читаем строки из консоли и отправляем их на сервер.
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			fmt.Print("> ")
			continue
		}

		out := map[string]string{"text": text}
		if err := conn.WriteJSON(out); err != nil {
			log.Println("write error:", err)
			return
		}
		fmt.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		log.Println("stdin error:", err)
	}
}

func getToken(host, name string) (string, error) {
	// getToken делает POST /auth и возвращает выданный сервером токен.
	// Токен запрашивается у сервера по HTTP: POST /auth { "name": "..." }.
	body := strings.NewReader(fmt.Sprintf(`{"name":%q}`, name))
	resp, err := http.Post("http://"+host+"/auth", "application/json", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth failed: %s", resp.Status)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", fmt.Errorf("auth failed: empty token")
	}
	return result.Token, nil
}
