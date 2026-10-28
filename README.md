# Go LLM Gateway

Высокопроизводительный отказоустойчивый API-шлюз (Reverse Proxy) для маршрутизации, защиты, кэширования и оптимизации запросов к большим языковым моделям (LLM), написанный на чистом **Go 1.26.2** без единой внешней зависимости (**Zero-Dependency**).

Проект решает главные инфраструктурные проблемы при интеграции ИИ в продакшен: падения внешних провайдеров, утечки чувствительных данных (PII) в промптах, повторную оплату идентичных запросов, неконтролируемый расход бюджета и различия в форматах API.

---

## Ключевые возможности и Архитектура

* **Zero-Dependency & Standard Library First:** Построен исключительно на базе стандартной библиотеки Go (`net/http`, `sync/atomic`, `crypto/sha256`, `bufio`, `regexp`, `log/slog`). Никаких сторонних фреймворков.
* **Lock-Free Circuit Breaker & Автоматический Fallback:** Реализация паттерна Circuit Breaker (состояния *Closed*, *Open*, *Half-Open*) на атомарных примитивах процессора (`sync/atomic`) без блокировок мьютексами. При таймауте или сбое основного провайдера (например, Gemini API) шлюз прозрачно для клиента переключает трафик на резервную модель.
* **In-Memory TTL Cache (`sync.RWMutex` + `SHA-256`):** Потокобезопасное кэширование синхронных ответов по детерминированному SHA-256 хэшу от параметров запроса (модель, температура, очищенные от PII сообщения). Идентичные запросы отдаются из памяти за 0 мс с заголовком `X-Cache: HIT` и фоновой очисткой просроченных записей.
* **Потоковый SSE-прокси (Real-Time Streaming):** Полная поддержка Server-Sent Events (`text/event-stream`). Чтение потока от провайдера через `bufio.Scanner` в отдельной горутине и мгновенная отправка чанков клиенту через низкоуровневый интерфейс `http.Flusher` с контролем отмены контекста.
* **PII Scrubber (Фильтрация секретов на лету):** Предварительно скомпилированный конвейер регулярных выражений (DFA/NFA), который до хэширования и отправки запроса во внешнюю сеть вырезает из промптов API-ключи, токены и адреса криптокошельков (`0x...`), заменяя их на `[REDACTED_PII]`.
* **In-Memory Rate Limiter (Token Bucket):** Потокобезопасный ограничитель скорости запросов по `X-API-Key` или IP-адресу клиента с ленивым математическим пересчетом токенов на базе монотонных часов Go и фоновым сборщиком мусора (cleanup loop).
* **Декларативная Low-Code маршрутизация:** Правила фоллбэков, таймауты и регулярные выражения безопасности настраиваются через единый JSON-конфиг (`config/routes.json`) без перекомпиляции кода.
* **Graceful Shutdown:** Корректный перехват системных сигналов (`SIGINT`, `SIGTERM`) с ожиданием завершения всех активных HTTP-соединений и фоновых воркеров.

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
3. [In-Memory Cache (SHA-256)] ──► (Если stream=false и ключ найден) ──► 200 OK (X-Cache: HIT)
   │ Кэш-промах (X-Cache: MISS) или stream=true
   ▼
4. [Router & Atomic Circuit Breaker]
   ├──► Таргет 1: Gemini API (Primary)
   │       ├── Успех ──► Сохраняет в кэш и проксирует JSON / SSE-стрим клиенту
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
│       └── main.go                  # Точка входа, DI, HTTP-сервер и Graceful Shutdown
├── config/
│   └── routes.json                  # Декларативная конфигурация маршрутов и безопасности
├── internal/
│   ├── config/
│   │   └── config.go                # Парсинг конфигурации
│   ├── domain/
│   │   └── models.go                # Контракты (интерфейс Provider) и DTO
│   ├── middleware/
│   │   ├── cache.go                 # Потокобезопасный In-Memory кэш с TTL (RWMutex + SHA-256)
│   │   ├── ratelimit.go             # Алгоритм Token Bucket + фоновая очистка памяти
│   │   ├── scrubber.go              # Потоковая очистка чувствительных данных (PII)
│   │   └── middleware_test.go       # Конкурентные Unit-тесты для RateLimiter, Cache и Scrubber
│   ├── providers/
│   │   ├── gemini.go                # Адаптер Google Gemini API (REST + SSE Stream)
│   │   └── mock.go                  # Резервный локальный провайдер для фоллбэка и тестов
│   └── proxy/
│       ├── handler.go               # HTTP-хендлеры, интеграция кэша и http.Flusher
│       ├── router.go                # Маршрутизация и Lock-Free Circuit Breaker
│       └── router_test.go           # Unit-тесты автоматов состояний Circuit Breaker и Fallback
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

   **Linux / macOS (Bash):**
   ```bash
   export GEMINI_API_KEY="ваш_ключ_при_наличии"
   go run ./cmd/gateway
   ```

   **Windows (PowerShell):**
   ```powershell
   $env:GEMINI_API_KEY="ваш_ключ_при_наличии"
   go run ./cmd/gateway
   ```

---

## Примеры использования

### 1. Синхронный запрос, PII Scrubber и проверка кэша (`X-Cache`)

**Linux / macOS / Git Bash:**
```bash
curl -i -X POST http://localhost:8080/v1/chat/completions \
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

**Windows (PowerShell через `curl.exe`):**
```powershell
'{"model":"smart-tier","stream":false,"messages":[{"role":"user","content":"Ошибка перевода с кошелька 0x71C7656EC7ab88b098defB751B7401B5f6d8976F. Ключ: api_key=sk_live_9876543210abcdef12345"}]}' | curl.exe -i -X POST http://localhost:8080/v1/chat/completions -H "Content-Type: application/json" -H "X-API-Key: win-client" -d "@-"
```

**Ответ шлюза:**
*(При первом запросе вернется заголовок `X-Cache: MISS`, при повторном идентичном запросе — мгновенный `X-Cache: HIT` из памяти, а секреты будут скрыты):*
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Cache: HIT

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

Передайте `"stream": true` и флаг `-N` для отключения буферизации:

**Windows (PowerShell):**
```powershell
'{"model":"smart-tier","stream":true,"messages":[{"role":"user","content":"Тестируем стрим! Секрет: token=ghp_1234567890abcdefghijklmnop"}]}' | curl.exe -N -X POST http://localhost:8080/v1/chat/completions -H "Content-Type: application/json" -d "@-"
```

**Поток событий (Server-Sent Events):**
```text
data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"[Fallback ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"Stream] ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"Получено ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"сообщение: ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"Тестируем ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"стрим! ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"Секрет: ","done":false}

data: {"id":"mock-stream-1793155...","provider":"mock","model":"fallback-local-v1","content":"[REDACTED_PII] ","done":false}

data: [DONE]
```

### 3. Проверка состояния сервера (Healthcheck)
```bash
curl.exe http://localhost:8080/healthz
# Ответ: {"status":"ok"}
```

---

## Тестирование и проверка конкурентности

Проект покрыт unit-тестами, проверяющими работу `RateLimiter`, `Cache`, `Scrubber` и переходы состояний `Circuit Breaker` под конкурентной нагрузкой из десятков горутин:

```bash
# Стандартный запуск всех тестов
go test -v ./...

# Запуск с детектором гонок данных (Data Race Detector)
go test -race -v ./...
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