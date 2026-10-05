# DHT Exporter

**Версия 1.1**

Prometheus-экспортёр для датчиков температуры и влажности DHT11/DHT22/AM2302.
Читает данные сенсоров из procfs-интерфейса, создаваемого [драйвером ядра dht](https://github.com/chapvic/dht-driver)
по пути `/proc/sensors/dht/gpio<pin>/`, и предоставляет их как метрики Prometheus
на HTTP-эндпоинте. Опционально записывает показания в JSON-файл для интеграции
с другими системами мониторинга.

Copyright (c) 2026, Chapvic

---

## Оглавление

- [Обзор](#обзор)
- [Драйвер ядра DHT](#драйвер-ядра-dht)
- [Требования](#требования)
- [Сборка](#сборка)
- [Установка](#установка)
- [Использование](#использование)
- [Файл конфигурации](#файл-конфигурации)
- [Параметры командной строки](#параметры-командной-строки)
- [Метрики Prometheus](#метрики-prometheus)
- [Вывод в JSON](#вывод-в-json)
  - [Структура JSON](#структура-json)
  - [Чтение JSON в Bash](#чтение-json-в-bash)
  - [Чтение JSON в Python](#чтение-json-в-python)
- [Запрос метрик](#запрос-метрик)
  - [Bash (curl + grep)](#bash-curl--grep)
  - [Python (urllib)](#python-urllib)
- [Служба systemd](#служба-systemd)
- [Логирование](#логирование)
- [Отслеживание доступности драйвера](#отслеживание-доступности-драйвера)
- [Лицензия](#лицензия)

---

## Обзор

DHT Exporter опрашивает данные сенсоров из `/proc/sensors/dht/gpio<pin>/value`
с настраиваемым интервалом и предоставляет их как метрики Prometheus на
HTTP-эндпоинте (по умолчанию `0.0.0.0:9988`). Также поддерживается запись
показаний в JSON-файл для интеграции с другими системами мониторинга.

Ключевые возможности:

- Чтение данных сенсоров из procfs-интерфейса, создаваемого драйвером ядра dht
- Предоставление метрик Prometheus на настраиваемом HTTP-эндпоинте
- Опциональный вывод в JSON-файл для интеграции с другими инструментами
- Динамическое отслеживание интервала: мониторинг `auto_interval` драйвера
  и автоматическая корректировка опроса при изменении
- Поддержка файла конфигурации: чтение `/etc/default/dht-exporter`
  (`PARAMS="..."`) с переопределением параметрами командной строки
- Управление интервалом драйвера: при явном указании `--interval` значение
  записывается обратно в `auto_interval` драйвера
- Корректное завершение по SIGINT/SIGTERM
- Цветное логирование в терминале, простой текст при перенаправлении вывода
- Таймауты чтения/записи HTTP для защиты ресурсов
- Отслеживание доступности драйвера: однократная запись в лог при исчезновении
  и появлении драйвера, без спама в логах

## Драйвер ядра DHT

DHT Exporter требует загруженного **драйвера ядра dht** с procfs-интерфейсом
по пути `/proc/sensors/dht/`. Исходный код драйвера доступен по адресу:

**https://github.com/chapvic/dht-driver**

Регистрация сенсоров — запись BCM-номеров пинов в `/proc/sensors/dht/export`:

```bash
echo 23 > /proc/sensors/dht/export
```

## Требования

- Go 1.21 или новее
- Загруженный [драйвер ядра dht](https://github.com/chapvic/dht-driver)
- Go-модуль `prometheus/client_golang` (автоматически загружается при сборке)

## Сборка

```bash
make
```

Создаёт статический бинарный файл `dht-exporter` с `CGO_ENABLED=0` и
удалёнными отладочными символами (`-ldflags="-s -w"`).

## Установка

```bash
sudo make install
```

Устанавливает:

- Бинарный файл в `/usr/local/bin/dht-exporter`
- systemd-юнит в `/etc/systemd/system/dht-exporter.service`
- Создаёт системного пользователя `dht-exporter`
- Включает службу для автозапуска

Запуск службы:

```bash
sudo make start
```

Или вручную:

```bash
sudo systemctl start dht-exporter
```

## Использование

Прямой запуск (требуется загруженный драйвер dht):

```bash
./dht-exporter --interval 10 --json /var/lib/dht-exporter/readings.json
```

Справка:

```bash
./dht-exporter --help
```

## Файл конфигурации

DHT Exporter читает опциональный файл конфигурации по пути
`/etc/default/dht-exporter`. Файл использует простой синтаксис `PARAMS="..."`:

```bash
# /etc/default/dht-exporter
PARAMS="--interval 15 --json /var/lib/dht-exporter/readings.json"
```

Параметры из файла конфигурации обрабатываются первыми; аргументы командной
строки переопределяют их. Если файл не существует, программа работает как
обычно — предупреждение не выводится.

Пример:

```bash
# Файл конфигурации задаёт интервал 15
PARAMS="--interval 15"

# Переопределение через CLI: интервал становится 5
./dht-exporter --interval 5
```

## Параметры командной строки

| Параметр | По умолчанию | Описание |
|----------|--------------|-----------|
| `--name <имя>` | hostname | Имя экспортёра для метки Prometheus (санитизируется, fallback на hostname) |
| `--addr <адрес>` | `0.0.0.0:9988` | Адрес HTTP-слушателя |
| `--interval <сек>` | 10 | Интервал опроса 2-60; при явном указании записывается в `auto_interval` драйвера |
| `--json <путь>` | отключено | Запись показаний в JSON-файл |
| `--driver <имя>` | `dht` | Имя модуля ядра для modprobe |
| `--rt <сек>` | 5 | Таймаут чтения HTTP 1-60 |
| `--wt <сек>` | 10 | Таймаут записи HTTP 1-120 (должен быть >= `--rt`) |
| `--help` | — | Показать справку |

## Метрики Prometheus

| Метрика | Метки | Описание |
|---------|-------|-----------|
| `dht_temperature_celsius` | name, pin | Температура в градусах Цельсия |
| `dht_humidity_percent` | name, pin | Относительная влажность в процентах |
| `dht_status_code` | name, pin | Код статуса сенсора (0 = успех) |
| `dht_timestamp_seconds` | name, pin | Unix-время последнего измерения |
| `dht_info` | name, pin, type, status | Информация о сенсоре (всегда 1) |

## Вывод в JSON

При указании `--json <путь>` экспортёр записывает показания сенсоров в
JSON-файл при каждом цикле опроса.

### Структура JSON

```json
{
  "timestamp": 1727457643,
  "sensors": [
    {
      "pin": 23,
      "humidity": 50.9,
      "temperature": 24.2,
      "status_code": 0,
      "timestamp": 1727457643,
      "info": {
        "sensor": "DHT22",
        "registered": 1727457369
      },
      "status_text": "success"
    }
  ]
}
```

Оба поля `timestamp` (время экспорта) и `info.registered` (время регистрации
сенсора) — Unix-время (`int64`).

### Чтение JSON в Bash

```bash
# Разобрать последнее чтение с помощью jq
jq '.sensors[] | {pin, temperature, humidity, status_text}' /var/lib/dht-exporter/readings.json

# Получить температуру для конкретного пина
jq -r '.sensors[] | select(.pin == 23) | .temperature' /var/lib/dht-exporter/readings.json

# Преобразовать время регистрации в читаемый формат
jq -r '.sensors[].info.registered | todate' /var/lib/dht-exporter/readings.json
```

### Чтение JSON в Python

```python
import json
from datetime import datetime

with open('/var/lib/dht-exporter/readings.json') as f:
    data = json.load(f)

print(f"Время экспорта: {datetime.fromtimestamp(data['timestamp'])}")

for sensor in data['sensors']:
    registered = datetime.fromtimestamp(sensor['info']['registered'])
    print(f"Пин {sensor['pin']}: {sensor['temperature']}°C, "
          f"{sensor['humidity']}% — {sensor['status_text']}")
    print(f"  Сенсор: {sensor['info']['sensor']}, "
          f"зарегистрирован: {registered}")
```

## Запрос метрик

### Bash (curl + grep)

```bash
# Получить все метрики
curl -s http://localhost:9988/metrics

# Получить температуру для пина 23
curl -s http://localhost:9988/metrics | grep 'dht_temperature_celsius.*pin="23"'

# Проверка состояния
curl -s http://localhost:9988/health
```

### Python (urllib)

```python
from urllib.request import urlopen

# Получить метрики
with urlopen('http://localhost:9988/metrics') as response:
    metrics = response.read().decode()

for line in metrics.splitlines():
    if line.startswith('dht_temperature_celsius') and 'pin="23"' in line:
        print(line)

# Проверка состояния
with urlopen('http://localhost:9988/health') as response:
    print(response.read().decode())
```

## Служба systemd

Файл службы использует `Type=exec` — systemd считает службу активной после
успешного запуска бинарного файла. Это избавляет от необходимости sd_notify
и обеспечивает надёжное определение запуска.

Служба работает от имени выделенного пользователя `dht-exporter` с
`CAP_SYS_MODULE` для доступа к modprobe. Вывод в JSON следует выполнять в
директорию `/var/lib/dht-exporter/` (StateDirectory), так как
`ProtectSystem=strict` делает большую часть файловой системы доступной
только для чтения.

Управление службой:

```bash
sudo systemctl start dht-exporter     # Запуск
sudo systemctl stop dht-exporter      # Остановка
sudo systemctl restart dht-exporter   # Перезапуск
sudo systemctl status dht-exporter    # Статус
journalctl -u dht-exporter -f          # Логи в реальном времени
```

## Логирование

Экспортёр использует структурированное логирование с метками времени
в формате `YYYY/MM/DD HH:MM:SS`:

```
2026/09/27 18:24:11 [INFO] DHT Exporter (v1.1) starting...
2026/09/27 18:24:11 [INFO]   interval:  10 seconds (from driver)
2026/09/27 18:24:11 [INFO] first reading: 1 sensor(s) registered
2026/09/27 18:24:11 [INFO] gpio23: T=24.2 C, H=50.9%, status: success
```

ANSI-цвета используются при выводе в терминал; простой текст — при
перенаправлении в файл или пайп.

## Отслеживание доступности драйвера

При выгрузке драйвера ядра dht (например, `rmmod dht`) экспортёр записывает
одно сообщение `[WARN] DHT driver not loaded` и продолжает опрос.
При повторной загрузке драйвера записывается `[INFO] DHT driver ready`,
затем показания сенсоров как при запуске. Метрики последнего успешного опроса
сохраняются во время отсутствия драйвера.

## Лицензия

Лицензировано под [GNU GPLv3](LICENSE).
