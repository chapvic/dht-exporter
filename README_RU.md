# DHT Exporter

Prometheus-экспортер для датчиков температуры и влажности DHT11/DHT22/AM2302.  
Считывает данные сенсоров из procfs-интерфейса, создаваемого [драйвером ядра DHT](https://github.com/chapvic/dht-driver) по пути `/proc/sensors/dht/gpio<pin>/`, и предоставляет их как метрики Prometheus на HTTP-эндпоинте. Опционально записывает показания в JSON-файл для интеграции с другими системами мониторинга.

**Версия:** 1.0  
**Лицензия:** GNU GPLv3  
**Автор:** Chapvic  

---

## Оглавление

- [Обзор](#обзор)
- [Драйвер ядра DHT](#драйвер-ядра-dht)
- [Возможности](#возможности)
- [Требования](#требования)
- [Сборка](#сборка)
- [Установка](#установка)
- [Управление сервисом](#управление-сервисом)
- [Параметры командной строки](#параметры-командной-строки)
- [Метрики Prometheus](#метрики-prometheus)
- [JSON-вывод](#json-вывод)
- [Примеры использования](#примеры-использования)
  - [Bash: чтение метрик через curl](#bash-чтение-метрик-через-curl)
  - [Bash: разбор JSON-вывода через jq](#bash-разбор-json-вывода-через-jq)
  - [Python: чтение метрик](#python-чтение-метрик)
  - [Python: разбор JSON-вывода](#python-разбор-json-вывода)
- [Отслеживание доступности драйвера](#отслеживание-доступности-драйвера)
- [Лицензия](#лицензия)

---

## Обзор

DHT Exporter связывает драйвер ядра DHT и Prometheus. Драйвер предоставляет показания сенсоров через procfs-интерфейс — экспортер считывает этот интерфейс с настраиваемым интервалом и отдаёт данные как стандартные метрики Prometheus. Это позволяет мониторить температуру и влажность с датчиков DHT11, DHT22 и AM2302 с помощью любого стека мониторинга, совместимого с Prometheus (Grafana, VictoriaMetrics и др.).

Экспортер также поддерживает запись показаний сенсоров в JSON-файл, который может использоваться скриптами, дашбордами и другими инструментами, не работающими с Prometheus.

---

## Драйвер ядра DHT

DHT Exporter требует загруженного **драйвера ядра DHT** и зарегистрированных сенсоров. Драйвер создаёт procfs-интерфейс по пути `/proc/sensors/dht/`, где каждый зарегистрированный сенсор представлен подкаталогом (`gpio<pin>/`) со следующими файлами:

| Файл | Описание |
|------|----------|
| `value` | Влажность и температура в формате `H=<h>\nT=<t>` |
| `status_code` | Числовой код статуса (0 = успех) |
| `status_text` | Текст статуса, читаемый человеком |
| `timestamp` | Unix-timestamp последнего измерения |
| `info` | Тип сенсора и время регистрации |

Драйвер также предоставляет глобальный файл `auto_interval`, управляющий интервалом опроса всех сенсоров.

**Исходный код драйвера:** [https://github.com/chapvic/dht-driver](https://github.com/chapvic/dht-driver)

Для регистрации сенсора запишите номер BCM-пина в `/proc/sensors/dht/export`:

```bash
echo 23 > /proc/sensors/dht/export
```

Для отмены регистрации:

```bash
echo 23 > /proc/sensors/dht/unexport
```

---

## Возможности

- Чтение данных сенсоров из `/proc/sensors/dht/gpio<pin>/value`
- Предоставление метрик Prometheus на настраиваемом HTTP-эндпоинте
- Опциональный вывод в JSON-файл для интеграции с другими инструментами
- Динамическое отслеживание интервала: мониторит `auto_interval` драйвера и автоматически подстраивает опрос
- Корректное завершение по SIGINT/SIGTERM
- Цветное логирование в терминале, обычный текст при перенаправлении вывода
- Санитизация имени сенсора с откатом на имя хоста
- Таймауты HTTP на чтение/запись для защиты ресурсов
- Немедленный первый опрос при запуске, затем периодический по интервалу
- JSON-вывод со структурированной информацией о сенсоре (тип + время регистрации в формате Unix timestamp)
- Отслеживание доступности драйвера: однократное сообщение при исчезновении и появлении драйвера, без засорения лога

---

## Требования

- Ядро Linux с загруженным драйвером DHT
- Go 1.21+ (для сборки)
- Библиотека Prometheus client_golang (автоматически обрабатывается через `go mod`)
- systemd (для управления сервисом, опционально)

---

## Сборка

```bash
make
```

Команда инициализирует Go-модуль при необходимости, обновляет зависимости и собирает статический бинарник со стрипнутыми символами (`CGO_ENABLED=0`, `-ldflags="-s -w"`).

Готовый бинарник — `dht-exporter` в текущей директории.

---

## Установка

```bash
sudo make install
```

Выполняемые шаги:

1. Установка бинарника в `/usr/local/bin/dht-exporter`
2. Установка systemd-юнита в `/etc/systemd/system/dht-exporter.service`
3. Создание выделенного пользователя `dht-exporter` (без входа, без оболочки)
4. Перезагрузка systemd и включение сервиса для автозапуска

Для удаления:

```bash
sudo make uninstall
```

---

## Управление сервисом

| Команда | Действие |
|---------|----------|
| `make start` | Запуск сервиса |
| `make stop` | Остановка сервиса |
| `make restart` | Перезапуск сервиса |
| `make status` | Показать статус сервиса |
| `make logs` | Просмотр логов в реальном времени (`journalctl -f`) |

Systemd-юнит использует `Type=exec` — systemd считает сервис активным сразу после запуска бинарника, без необходимости sd_notify. Сервис работает от имени выделенного непривилегированного пользователя с правом `CAP_SYS_MODULE` (для modprobe) и включёнными параметрами безопасности.

---

## Параметры командной строки

| Параметр | По умолчанию | Описание |
|----------|--------------|----------|
| `--name <имя>` | имя хоста | Имя экспортера, используемое как метка Prometheus. Спецсимволы удаляются; если пусто после санитизации, используется имя хоста. |
| `--addr <адрес>` | `0.0.0.0:9988` | Адрес прослушивания HTTP. |
| `--interval <сек>` | `10` | Интервал опроса в секундах. Диапазон: 2–60. Установите 0 для использования `auto_interval` драйвера. |
| `--json <путь>` | отключено | Запись показаний сенсоров в JSON-файл. Обновляется при каждом опросе. Типовые директории: `/etc/`, `/opt/`, `/run/`, `/tmp/`, `/var/lib/`, `/var/log/`, `/usr/local/etc/`. |
| `--driver <имя>` | `dht` | Имя модуля ядра для modprobe, если procfs-интерфейс не найден при запуске. |
| `--rt <сек>` | `5` | Таймаут HTTP на чтение. Диапазон: 1–60. |
| `--wt <сек>` | `10` | Таймаут HTTP на запись. Диапазон: 1–120. Должен быть >= `--rt`. |
| `--help` | — | Показать справку. |

---

## Метрики Prometheus

| Метрика | Тип | Метки | Описание |
|---------|-----|-------|----------|
| `dht_temperature_celsius` | Gauge | `name`, `pin` | Температура в градусах Цельсия |
| `dht_humidity_percent` | Gauge | `name`, `pin` | Относительная влажность в процентах |
| `dht_status_code` | Gauge | `name`, `pin` | Код статуса сенсора (0 = успех) |
| `dht_timestamp_seconds` | Gauge | `name`, `pin` | Unix-timestamp последнего измерения |
| `dht_info` | Gauge | `name`, `pin`, `type`, `status` | Информация о сенсоре (всегда 1) |

---

## JSON-вывод

Если указан `--json <путь>`, экспортер записывает JSON-файл при каждом опросе. Формат:

```json
{
  "timestamp": 1727462445,
  "sensors": [
    {
      "pin": 23,
      "humidity": 50.9,
      "temperature": 24.2,
      "status_code": 0,
      "timestamp": 1727462443,
      "info": {
        "sensor": "DHT22",
        "registered": 1727374000
      },
      "status_text": "SUCCESS"
    }
  ]
}
```

| Поле | Тип | Описание |
|------|-----|----------|
| `timestamp` | int64 | Unix-timestamp записи JSON-файла |
| `sensors` | массив | По одному элементу на каждый зарегистрированный сенсор |
| `sensors[].pin` | int | Номер BCM GPIO-пина |
| `sensors[].humidity` | float64 | Относительная влажность в процентах |
| `sensors[].temperature` | float64 | Температура в градусах Цельсия |
| `sensors[].status_code` | int | Код статуса (0 = успех) |
| `sensors[].timestamp` | int64 | Unix-timestamp последнего измерения сенсора |
| `sensors[].info.sensor` | string | Тип сенсора (напр. "DHT22", "DHT11") |
| `sensors[].info.registered` | int64 | Время регистрации сенсора в формате Unix timestamp |
| `sensors[].status_text` | string | Текст статуса, читаемый человеком (напр. "SUCCESS") |

---

## Примеры использования

### Bash: чтение метрик через curl

```bash
# Получить все метрики Prometheus
curl http://localhost:9988/metrics

# Только температура
curl -s http://localhost:9988/metrics | grep dht_temperature

# Проверка работоспособности
curl http://localhost:9988/health
```

### Bash: разбор JSON-вывода через jq

```bash
# Прочитать последний JSON-файл и красиво вывести
jq . /var/lib/dht-exporter/readings.json

# Извлечь температуру и влажность для пина 23
jq '.sensors[] | select(.pin == 23) | {temp: .temperature, hum: .humidity}' \
  /var/lib/dht-exporter/readings.json

# Получить все пины и их статус
jq '.sensors[] | {pin, status_text}' /var/lib/dht-exporter/readings.json

# Преобразовать Unix-timestamp в читаемый формат
jq '.sensors[] | {pin, registered: (.info.registered | strftime("%Y-%m-%d %H:%M:%S"))}' \
  /var/lib/dht-exporter/readings.json
```

### Python: чтение метрик

```python
import urllib.request

def get_dht_metrics(host="localhost", port=9988):
    """Получить и разобрать метрики Prometheus от DHT Exporter."""
    url = f"http://{host}:{port}/metrics"
    with urllib.request.urlopen(url) as resp:
        data = resp.read().decode()

    metrics = {}
    for line in data.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        # Разбор: metric_name{labels} value
        name, _, rest = line.partition("{")
        labels_str, _, value = rest.rpartition("}")
        metric_key = name.strip()

        labels = {}
        for pair in labels_str.split(","):
            pair = pair.strip()
            if "=" in pair:
                k, v = pair.split("=", 1)
                labels[k.strip()] = v.strip().strip('"')

        pin = labels.get("pin", "?")
        metrics.setdefault(pin, {})[metric_key] = float(value)

    return metrics

metrics = get_dht_metrics()
for pin, data in metrics.items():
    temp = data.get("dht_temperature_celsius", "N/A")
    hum = data.get("dht_humidity_percent", "N/A")
    print(f"Пин {pin}: T={temp} C, H={hum}%")
```

### Python: разбор JSON-вывода

```python
import json
from datetime import datetime

def read_json(path="/var/lib/dht-exporter/readings.json"):
    """Чтение JSON-файла, записанного DHT Exporter."""
    with open(path) as f:
        data = json.load(f)

    print(f"Последнее обновление: {datetime.fromtimestamp(data['timestamp'])}")
    for s in data["sensors"]:
        registered = datetime.fromtimestamp(s["info"]["registered"])
        print(f"  Пин {s['pin']} ({s['info']['sensor']}):")
        print(f"    Температура:  {s['temperature']} C")
        print(f"    Влажность:    {s['humidity']}%")
        print(f"    Статус:       {s['status_text']}")
        print(f"    Регистрация:  {registered}")
        print(f"    Измерение:    {datetime.fromtimestamp(s['timestamp'])}")

read_json()
```

---

## Отслеживание доступности драйвера

Экспортер отслеживает, загружен ли драйвер ядра DHT и доступен ли procfs-интерфейс:

- **Драйвер исчез:** выводится одно сообщение `[WARN] DHT driver not loaded: /proc/sensors/dht/ not found`. Экспортер продолжает опрос молча, ожидая возвращения драйвера.
- **Драйвер снова доступен:** выводится одно сообщение `[INFO] DHT driver ready: /proc/sensors/dht`, за которым следуют показания сенсоров, как при запуске (`first reading: N sensor(s) registered`).

Это предотвращает засорение лога, гарантируя уведомление об изменениях состояния драйвера.

---

## Лицензия

GNU General Public License v3. См. [LICENSE](LICENSE).

Copyright (c) 2026, Chapvic.
