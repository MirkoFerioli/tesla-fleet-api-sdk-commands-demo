# Tesla API service

Il servizio Go espone una API HTTP locale sulla porta `8000`. Gestisce OAuth Tesla, chiama il `tesla-proxy`, invia comandi generici e scrive su InfluxDB.

Base URL dall'host:

```text
http://localhost:8000
```

Base URL dagli altri container nella stessa rete Docker:

```text
http://tesla-api:8000
```

## Differenza tra data, poll e latest

| Endpoint | Chiama Tesla | Scrive su InfluxDB | Uso principale |
| --- | ---: | ---: | --- |
| `GET /api/vehicle/data` | si | no | test, debug, lettura manuale immediata |
| `POST /api/vehicle/poll` | si | si | raccolta dati periodica da Node-RED |
| `GET /api/vehicle/latest` | no | no | leggere l'ultimo snapshot gia raccolto |

In pratica: usa `poll` per costruire lo storico, `data` per guardare cosa risponde Tesla senza salvare, e `latest` per leggere l'ultimo dato gia raccolto senza richiamare Tesla.

## GET /health

Verifica servizio, proxy e InfluxDB.

```bash
curl http://localhost:8000/health
```

Risposta esempio:

```json
{
  "status": "ok",
  "vehicle_vin_set": true,
  "proxy": "ok",
  "influxdb": "ok",
  "reset_token_on_start": true
}
```

## GET /api/vehicle/data

Legge `vehicle_data` dalla Tesla API. Non scrive su InfluxDB.

```bash
curl "http://localhost:8000/api/vehicle/data?endpoints=charge_state,drive_state"
```

## POST /api/vehicle/poll

Legge `vehicle_data` e scrive i dati su InfluxDB nella measurement `tesla_vehicle`.

```bash
curl -X POST http://localhost:8000/api/vehicle/poll \
  -H "Content-Type: application/json" \
  -d '{"endpoints":"charge_state,drive_state,climate_state,vehicle_state"}'
```

Body opzionale:

```json
{
  "endpoints": "charge_state,drive_state,climate_state,vehicle_state"
}
```

Se ometti `endpoints`, il servizio chiede tutti i dati disponibili a `vehicle_data`.

## GET /api/vehicle/latest

Restituisce l'ultimo snapshot ottenuto da `POST /api/vehicle/poll`.

```bash
curl http://localhost:8000/api/vehicle/latest
```

Se non hai ancora eseguito un poll, torna `404`.

## POST /api/vehicle/command

Endpoint generico per i comandi Tesla. Il nome comando viene passato nel payload.

```bash
curl -X POST http://localhost:8000/api/vehicle/command \
  -H "Content-Type: application/json" \
  -d '{"command":"door_lock","params":{},"wake":true}'
```

Schema:

```json
{
  "command": "nome_comando_tesla",
  "params": {},
  "wake": true
}
```

Esempi:

```json
{"command":"door_lock","params":{},"wake":true}
```

```json
{"command":"door_unlock","params":{},"wake":true}
```

```json
{"command":"set_sentry_mode","params":{"on":true},"wake":true}
```

```json
{"command":"set_sentry_mode","params":{"on":false},"wake":true}
```

Il servizio scrive l'audit del comando su InfluxDB nella measurement `tesla_command`.

## InfluxDB schema

Measurement `tesla_vehicle`:

- tag: `vin`
- tag: `endpoint`
- fields: valori flattenati dalla risposta Tesla, per esempio `charge_state_battery_level`

Measurement `tesla_command`:

- tag: `vin`
- tag: `command`
- tag: `status`
- field: `duration_ms`
- field: `http_status`
- field opzionale: `error`

## OAuth e reset token

Con `TESLA_RESET_TOKEN_ON_START=true`, il servizio pulisce `tesla-tokens.json` a ogni avvio. La prima chiamata a un endpoint Tesla stampa nei log un URL OAuth da aprire nel browser.

```bash
docker compose logs -f tesla-api
```
