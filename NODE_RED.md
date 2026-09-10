# Node-RED

Node-RED deve orchestrare automazioni chiamando `tesla-api`. Non deve scrivere su InfluxDB e non deve contenere token Tesla.

URL interno da usare nei nodi HTTP Request:

```text
http://tesla-api:8000
```

## Accesso web a Node-RED

Node-RED deve essere esposto sull'host se vuoi creare e modificare le automazioni dal browser.

Nel compose attuale e pubblicato cosi:

```yaml
ports:
  - "1880:1880"
```

Quindi l'editor web e disponibile da:

```text
http://localhost:1880
```

Questa esposizione serve al browser, non ai container. Per le chiamate interne Docker, Node-RED raggiunge `tesla-api` usando la rete condivisa `tesla-network` e l'hostname `http://tesla-api:8000`.

Se vuoi usare Node-RED solo dal computer locale, puoi restringere la porta all'interfaccia loopback:

```yaml
ports:
  - "127.0.0.1:1880:1880"
```

Se invece vuoi aprire l'editor Node-RED da altri dispositivi nella LAN, lascia:

```yaml
ports:
  - "1880:1880"
```

In quel caso configura autenticazione o proteggi l'accesso con VPN/reverse proxy: dall'editor Node-RED si possono creare flow che chiamano servizi interni.

## Flow polling dati

Obiettivo: leggere dati Tesla e farli salvare a Go su InfluxDB.

Nodi:

1. Inject, ogni 5-15 minuti.
2. HTTP Request, metodo `POST`.
3. URL `http://tesla-api:8000/api/vehicle/poll`.
4. Header `Content-Type: application/json`.
5. Body JSON con gli endpoint desiderati.
6. Debug o Switch sul risultato.

Payload consigliato:

```json
{
  "endpoints": "charge_state,drive_state,climate_state,vehicle_state"
}
```

Non usare polling troppo frequente: puo svegliare spesso il veicolo e consumare batteria.

## Flow comando generico

Obiettivo: inviare a Tesla un comando scelto dall'automazione.

HTTP Request:

```text
POST http://tesla-api:8000/api/vehicle/command
```

Header:

```text
Content-Type: application/json
```

Payload lock:

```json
{
  "command": "door_lock",
  "params": {},
  "wake": true
}
```

Payload unlock:

```json
{
  "command": "door_unlock",
  "params": {},
  "wake": true
}
```

Payload Sentry Mode on:

```json
{
  "command": "set_sentry_mode",
  "params": {
    "on": true
  },
  "wake": true
}
```

Payload Sentry Mode off:

```json
{
  "command": "set_sentry_mode",
  "params": {
    "on": false
  },
  "wake": true
}
```

## Flow ultimo stato

Usa questo endpoint se vuoi far decidere Node-RED sull'ultimo snapshot gia letto dal poll:

```text
GET http://tesla-api:8000/api/vehicle/latest
```

Questo non chiama Tesla e non scrive su InfluxDB.

## Test da container Node-RED

```bash
docker compose exec node-red wget -qO- http://tesla-api:8000/health
```

Se questo funziona, i nodi HTTP Request possono raggiungere la nuova API.
