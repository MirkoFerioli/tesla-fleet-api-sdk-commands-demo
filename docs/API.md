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

Restituisce sempre HTTP 200 e `status: ok` se l'handler riesce a rispondere. Leggi anche `proxy` e `influxdb`: contengono `ok`, un messaggio di errore o, per InfluxDB, `not_configured`. L'healthcheck Docker dell'API controlla la risposta HTTP, non questi campi. `vehicle_vin_set` indica solo la presenza del VIN, non la sua validita. Questo endpoint non verifica i token OAuth ne la raggiungibilita del veicolo.

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

Se ometti `endpoints`, il servizio non invia quel parametro al proxy: i dati restituiti dipendono dai default e dai permessi della Fleet API. Il valore nel body, se non vuoto, ha precedenza sul parametro query `endpoints`.

`data` e `poll` non eseguono un comando di risveglio esplicito: con il veicolo addormentato possono fallire. Se InfluxDB non e configurato, `poll` aggiorna comunque lo snapshot in memoria e restituisce `written: true`, ma non salva dati nel database. Se invece una scrittura InfluxDB configurata fallisce, restituisce HTTP 502 e non aggiorna lo snapshot.

## GET /api/vehicle/latest

Restituisce l'ultimo snapshot ottenuto da `POST /api/vehicle/poll`.

```bash
curl http://localhost:8000/api/vehicle/latest
```

Se non hai ancora eseguito un poll riuscito, torna `404`. Lo snapshot esiste solo in memoria e si perde al riavvio; `GET /api/vehicle/data` non lo aggiorna.

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

Se omessi, `params` diventa `{}` e `wake` vale `true`. Con `wake: false` il servizio non tenta il risveglio. Il nome comando puo contenere solo lettere, numeri, underscore e trattini; il supporto del comando e la validita dei parametri dipendono dal proxy e dalla Fleet API.

Il servizio tenta di scrivere l'audit del comando su InfluxDB nella measurement `tesla_command`, se configurato. Un errore di audit viene registrato nei log senza cambiare la risposta del comando. La risposta HTTP 200 dell'API indica un HTTP 2xx dal proxy: controlla anche `response`, perche un risultato Tesla puo contenere `result: false`.

Payload JSON non validi e nomi comando non ammessi producono HTTP 400; errori OAuth, Tesla o delle scritture del polling producono HTTP 502 con un campo JSON `error`.

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

Nel Compose questo valore e imposto esplicitamente nell'environment di `tesla-api`. Per conservare i token imposta `false` in `docker-compose.yml` e ricrea il servizio; modificarlo soltanto in `.env` non basta. Con reset disabilitato il servizio riusa un access token ancora valido e tenta il refresh prima di avviare un nuovo OAuth.

```bash
docker compose logs -f tesla-api
```

La richiesta Tesla puo rimanere in attesa: consulta i log da un secondo terminale. La porta callback `8080` ascolta solo durante OAuth e non e un endpoint di health. L'attesa OAuth arriva a cinque minuti, ma il server API ha un write timeout di due minuti: se la richiesta iniziale termina prima dell'autorizzazione, ripetila dopo aver completato il login.
