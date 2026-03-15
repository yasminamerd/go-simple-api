package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Message — структура нашего сообщения в чате
type Message struct {
	User      string `json:"user"`      // Кто отправил
	Text      string `json:"text"`      // Текст сообщения
	Timestamp string `json:"timestamp"` // Время отправки
}

// ChatServer — структура, которая хранит все наши чат-комнаты
type ChatServer struct {
	// mu — это тот самый "замок" (мьютекс) для многопоточности. 
	// Он защищает память, когда несколько пользователей пишут одновременно.
	mu       sync.Mutex 
	// messages — это словарь. Ключ (строка) — название комнаты, а значение — список сообщений.
	messages map[string][]Message
}

// Создаем наш сервер (пока он пустой)
var chat = ChatServer{
	messages: make(map[string][]Message),
}

// Функция, которая обрабатывает запросы к чату
func chatHandler(w http.ResponseWriter, r *http.Request) {
	// 1. АУТЕНТИФИКАЦИЯ
	// Читаем заголовок X-Nickname. Это наш способ узнать, кто пришел (без сложного логина)
	nickname := r.Header.Get("X-Nickname")
	if nickname == "" {
		// Если ника нет, возвращаем ошибку 401 (Не авторизован)
		http.Error(w, "Ошибка: Добавь заголовок X-Nickname", http.StatusUnauthorized)
		return
	}

	// 2. ВЫБОР КОМНАТЫ
	// Берем название комнаты из ссылки, например /chat?room=univer
	room := r.URL.Query().Get("room")
	if room == "" {
		room = "general" // Если комната не указана, кидаем всех в общую
	}

	// 3. ЕСЛИ ЭТО GET-ЗАПРОС (Чтение сообщений)
	if r.Method == http.MethodGet {
		chat.mu.Lock()                     // Закрываем замок (чтобы никто не добавил сообщение, пока мы читаем)
		roomMessages := chat.messages[room] // Берем сообщения нужной комнаты
		chat.mu.Unlock()                   // Открываем замок

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(roomMessages) // Отправляем сообщения в виде JSON
		return
	}

	// 4. ЕСЛИ ЭТО POST-ЗАПРОС (Отправка сообщения)
	if r.Method == http.MethodPost {
		// Создаем временную структуру для чтения текста из запроса
		var input struct {
			Text string `json:"text"`
		}
		
		// Читаем то, что пользователь прислал в теле запроса (Body)
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Ошибка чтения сообщения", http.StatusBadRequest)
			return
		}

		// Формируем готовое сообщение
		msg := Message{
			User:      nickname,
			Text:      input.Text,
			Timestamp: time.Now().Format("15:04:05"), // Форматируем время в часы:минуты:секунды
		}

		// Сохраняем сообщение
		chat.mu.Lock() // Снова закрываем замок перед изменением памяти!
		chat.messages[room] = append(chat.messages[room], msg) // Добавляем сообщение в список комнаты
		chat.mu.Unlock() // Открываем замок

		w.WriteHeader(http.StatusCreated) // Отвечаем статусом 201 (Создано)
		fmt.Fprintln(w, "Сообщение отправлено в комнату", room)
		return
	}

	// Если метод не GET и не POST
	http.Error(w, "Используйте GET для чтения или POST для отправки", http.StatusMethodNotAllowed)
}

func main() {
	// Регистрируем наш обработчик по адресу /chat
	http.HandleFunc("/chat", chatHandler)

	fmt.Println("Сервер чата запущен на http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Ошибка:", err)
	}
}