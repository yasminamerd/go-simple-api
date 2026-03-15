package main // Каждый проект на Go начинается с пакета main - это точка входа в программу

import (
	"encoding/json" // Пакет для работы с форматом JSON (в нем общаются API)
	"fmt"           // Пакет для форматированного вывода текста (например, в консоль)
	"net/http"      // Встроенный в Go пакет для создания веб-сервера
)

// Info — это структура (как класс или объект), описывающая данные, которые мы будем отдавать.
// В обратных кавычках `json:"id"` мы говорим, как это поле будет называться в JSON-ответе.
type Info struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

func main() {
	// 1. Создаем первый "эндпоинт" (путь). Это просто главная страница.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// w - это то, что мы отвечаем (Write). r - это запрос от клиента (Read/Request).
		fmt.Fprintln(w, "Привет! Мое АПИ работает.")
	})

	// 2. Создаем путь "/api/data", который будет отдавать информацию в формате JSON.
	http.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		// Говорим браузеру или Postman, что мы возвращаем именно JSON, а не просто текст
		w.Header().Set("Content-Type", "application/json")

		// Создаем немного фейковых данных (массив из структур Info)
		data :=[]Info{
			{ID: 1, Title: "Универ", Message: "Завтра нужно показать этот код"},
			{ID: 2, Title: "Go", Message: "Оказывается, писать АПИ на Go довольно просто"},
		}

		// Превращаем наши данные в JSON и отправляем клиенту (в w)
		json.NewEncoder(w).Encode(data)
	})

	// 3. Запускаем сервер на порту 8080
	fmt.Println("Сервер запущен... Открой http://localhost:8080 в браузере или Postman")
	// http.ListenAndServe "слушает" порт и держит сервер включенным
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Ошибка запуска сервера:", err)
	}
}