# Go LLM Gateway

Высокопроизводительный отказоустойчивый API-шлюз (Reverse Proxy) для маршрутизации, защиты и оптимизации запросов к большим языковым моделям (LLM), написанный на чистом **Go 1.26+** без единой внешней зависимости (**Zero-Dependency**).

Проект решает главные инфраструктурные проблемы при интеграции ИИ в продакшен: падения внешних провайдеров, утечки чувствительных данных (PII) в промптах, неконтролируемый расход бюджета и различия в форматах API.

---

## Ключевые возможности и Архитектура

* **Zero-Dependency & Standard Library First:** Построен исключительно на базе стандартной библиотеки Go (`net/http`, `sync/atomic`, `bufio`, `regexp`, `log/slog`). Никаких раздутых фреймворков.
* **Lock-Free Circuit Breaker & Автоматический Fallback:** Реализация паттерна Circuit Breaker (состояния *Closed*, *Open*, *Half-Open*) на атомарных примитивах процессора (`sync/atomic`) без блокировок мьютексами. При таймауте или сбое основного провайдера (например, Gemini API) шлюз прозрачно для клиента переключает трафик на резервную модель.
* **Потоковый SSE-прокси (Real-Time Streaming):** Полная поддержка Server-Sent Events (`text/event-stream`). Чтение потока от провайдера через `bufio.Scanner` в отдельной горутине и мгновенная отправка чанков клиенту через низкоуровневый интерфейс `http.Flusher` с контролем отмены контекста.
* **PII Scrubber (Фильтрация секретов на лету):** Предварительно скомпилированный конвейер регулярных выражений (DFA/NFA), который до отправки запроса во внешнюю сеть вырезает из промптов API-ключи, токены и адреса криптокошельков (`0x...`), заменяя их на `[REDACTED_PII]`.
* **In-Memory Rate Limiter (Token Bucket):** Потокобезопасный ограничитель скорости запросов по `X-API-Key` или IP-адресу клиента с ленивым математическим пересчетом токенов на базе монотонных часов Go и фоновым сборщиком мусора (cleanup loop).
* **Декларативная Low-Code маршрутизация:** Правила фоллбэков, таймауты и регулярные выражения безопасности настраиваются через единый JSON-конфиг (`config/routes.json`) без перекомпиляции кода.
* **Graceful Shutdown:** Корректный перехват системных сигналов (`SIGINT`, `SIGTERM`) с ожиданием завершения всех активных стримов.

---

## Схема работы конвейера (Request Pipeline)

```text
[Клиент / Микросервис] 
   │  POST /v1/chat/completions (модель: "smart-tier")
   ▼
1. [Rate Limiter (Token Bucket)] ──► Лимит превышен? ──► 429 Too Many Requests
   │ OK
   ▼
2. [PII Scrubber] ──► Маскирует API-ключи, пароли и 0x-кошельки ──► [REDACTED_PII]
   │
   ▼
3. [Router & Atomic Circuit Breaker]
   ├──► Таргет 1: Gemini API (Primary)
   │       ├── Успех ──► Проксирует JSON / SSE-стрим клиенту
   │       └── Сбой / Таймаут ──► Инкремент atomic-счетчика ошибок
   │
   └──► Таргет 2: Fallback Provider ──► Бесшовно отдает ответ клиенту
```

---

## Структура проекта

```text
LLM-Gateway/
├── cmd/
│   └── gateway/
│       └── main.go              # Точка входа, DI, HTTP-сервер и Graceful Shutdown
├── config/
│   └── routes.json              # Декларативная конфигурация маршрутов и безопасности
├── internal/
│   ├── config/
│   │   └── config.go            # Парсинг конфигурации
│   ├── domain/
│   │   └── models.go            # Контракты (интерфейс Provider) и DTO
│   ├── middleware/
│   │   ├── ratelimit.go         # Алгоритм Token Bucket + фоновая очистка памяти
│   │   └── scrubber.go          # Потоковая очистка чувствительных данных (PII)
│   ├── providers/
│   │   ├── gemini.go            # Адаптер Google Gemini API (REST + SSE Stream)
│   │   └── mock.go              # Резервный локальный провайдер для фоллбэка и тестов
│   └── proxy/
│       ├── handler.go           # HTTP-хендлеры и управление http.Flusher
│       └── router.go            # Маршрутизация и Lock-Free Circuit Breaker
└── go.mod
```

---

## Быстрый старт

### Требования
* **Go 1.26.2** или выше.

### Запуск сервера

1. Клонируйте репозиторий:
   ```bash
   git clone [https://github.com/syydee3/LLM-Gateway.git](https://github.com/syydee3/LLM-Gateway.git)
   cd LLM-Gateway
   ```

2. Запустите шлюз (с ключом Gemini или без него — при отсутствии ключа автоматически сработает Circuit Breaker и переключит трафик на локальный Fallback-провайдер):
   ```bash
   export GEMINI_API_KEY="ваш_ключ_при_наличии"
   go run ./cmd/gateway
   ```

---

## Примеры использования

### 1. Синхронный запрос с проверкой защиты секретов (PII Scrubber)

Отправим запрос, содержащий секретный API-ключ и адрес Ethereum-кошелька:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-API-Key: backend-service-01" \
  -d '{
    "model": "smart-tier",
    "stream": false,
    "messages": [
      {
        "role": "user",
        "content": "Ошибка перевода с кошелька 0x71C7656EC7ab88b098defB751B7401B5f6d8976F. Ключ: api_key=sk_live_9876543210abcdef12345"
      }
    ]
  }'
```

**Ответ шлюза (секреты автоматически вырезаны до отправки в LLM):**
```json
{
  "id": "mock-1761600392847192000",
  "provider": "mock",
  "model": "fallback-local-v1",
  "message": {
    "role": "assistant",
    "content": "[Fallback Mock Response] Обработан текст: Ошибка перевода с кошелька [REDACTED_PII]. Ключ: [REDACTED_PII]"
  }
}
```

### 2. Потоковая генерация в реальном времени (SSE Streaming)

Передайте `"stream": true` и флаг `-N` в `curl` для отключения буферизации:

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-API-Key: frontend-client" \
  -d '{
    "model": "smart-tier",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Привет! Проверяем потоковую передачу токенов."}
    ]
  }'
```

**Поток событий (Server-Sent Events):**
```text
data: {"id":"mock-stream-17616...","provider":"mock","model":"fallback-local-v1","content":"[Fallback ","done":false}

data: {"id":"mock-stream-17616...","provider":"mock","model":"fallback-local-v1","content":"Stream] ","done":false}

data: {"id":"mock-stream-17616...","provider":"mock","model":"fallback-local-v1","content":"Получено ","done":false}

data: [DONE]
```

### 3. Проверка состояния сервера (Healthcheck)
```bash
curl http://localhost:8080/healthz
# Ответ: {"status":"ok"}
```

---

## Конфигурация (`config/routes.json`)

Вы можете добавлять собственные виртуальные модели, настраивать пороги срабатывания предохранителя (`max_failures`), время охлаждения (`cooldown_sec`) и регулярные выражения для фильтрации данных:

```json
{
  "port": 8080,
  "read_timeout_sec": 15,
  "write_timeout_sec": 120,
  "rate_limit_rpm": 60,
  "scrub_pii": true,
  "pii_patterns": [
    "(?i)(api[_-]?key|secret|token)[\\s:=]+[\"'`]?[a-zA-Z0-9_\\-]{16,}[\"'`]?",
    "\\b0x[a-fA-F0-9]{40}\\b"
  ],
  "virtual_models": {
    "smart-tier": {
      "targets": [
        {
          "provider": "gemini",
          "model": "gemini-2.5-flash",
          "timeout_ms": 10000,
          "max_failures": 2,
          "cooldown_sec": 30
        },
        {
          "provider": "mock",
          "model": "fallback-local-v1",
          "timeout_ms": 5000,
          "max_failures": 5,
          "cooldown_sec": 10
        }
      ]
    }
  }
}
```