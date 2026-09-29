# Avvio stack Tesla API, InfluxDB, Node-RED e Grafana

Questa guida avvia tutto lo stack Docker:

- `tesla-proxy`: proxy ufficiale Tesla Vehicle Command, usato per firmare e inoltrare le chiamate.
- `tesla-api`: servizio Go HTTP che legge la Tesla API, invia comandi generici e scrive direttamente su InfluxDB.
- `influxdb`: database time-series per storico veicolo e audit comandi.
- `node-red`: automazioni che chiamano la nuova API Go.
- `grafana`: dashboard su InfluxDB.

Tutti i container stanno sulla stessa rete Docker `tesla-network`. Dall'esterno usi lo stesso IP del computer e porte diverse.

## 1. Prerequisiti

- Docker e Docker Compose.
- `TESLA_CLIENT_ID` e `TESLA_CLIENT_SECRET` da developer.tesla.com.
- VIN del veicolo.
- `private-key.pem` nella root del progetto.
- Redirect URI Tesla configurata come `http://localhost:8080/callback`.

## 2. Configurazione Tesla

```bash
cp .env.example .env
```

Modifica `.env`:

```ini
TESLA_CLIENT_ID=xxx
TESLA_CLIENT_SECRET=xxx
TESLA_REDIRECT_URI=http://localhost:8080/callback
TESLA_VEHICLE_VIN=xxx
```

Il compose imposta anche queste variabili per il servizio API:

```ini
TESLA_API_LISTEN_ADDR=:8000
TESLA_PROXY_URL=https://tesla-proxy:4443
TESLA_TOKEN_PATH=/app/tesla-tokens.json
TESLA_RESET_TOKEN_ON_START=true
INFLUX_URL=http://influxdb:8086
INFLUX_ORG=tesla
INFLUX_BUCKET=vehicle
```

Con `TESLA_RESET_TOKEN_ON_START=true`, a ogni riavvio il servizio Go pulisce `tesla-tokens.json`. Questo forza un nuovo OAuth ed evita il blocco che si verifica quando l'app trova un token gia presente.

## 3. Preparazione proxy

```bash
bash setup-proxy.sh
touch tesla-tokens.json
```

`setup-proxy.sh` crea:

- `config/tls-cert.pem`
- `config/tls-key.pem`
- `config/fleet-key.pem`

## 4. Avvio completo

```bash
docker compose up --build -d
```

Controlla lo stato:

```bash
docker compose ps
docker compose logs -f tesla-api
```

Porte esposte sull'host:

| Servizio | URL |
|---|---|
| Tesla API Go | `http://localhost:8000` |
| Callback OAuth | `http://localhost:8080/callback` |
| Tesla proxy | `https://localhost:4443` |
| Node-RED | `http://localhost:1880` |
| InfluxDB | `http://localhost:8086` |
| Grafana | `http://localhost:3000` |
| Mosquitto MQTT | `mqtt://localhost:1883` |
| MySQL | `localhost:3306` |

## 5. Prima autenticazione OAuth

Il servizio `tesla-api` parte senza bloccare l'avvio. Il flusso OAuth parte quando chiami un endpoint che richiede Tesla, per esempio:

```bash
curl -X POST http://localhost:8000/api/vehicle/poll
```

Poi guarda i log:

```bash
docker compose logs -f tesla-api
```

Apri nel browser l'URL Tesla stampato nei log. Dopo il login Tesla, il callback torna su `http://localhost:8080/callback` e il servizio salva il nuovo `tesla-tokens.json`.

## 6. Chiamate principali

Health check:

```bash
curl http://localhost:8000/health
```

Polling dati e scrittura su InfluxDB:

```bash
curl -X POST http://localhost:8000/api/vehicle/poll \
  -H "Content-Type: application/json" \
  -d '{"endpoints":"charge_state,drive_state,climate_state,vehicle_state"}'
```

Comando generico Tesla:

```bash
curl -X POST http://localhost:8000/api/vehicle/command \
  -H "Content-Type: application/json" \
  -d '{"command":"door_lock","params":{},"wake":true}'
```

Da Node-RED, usa gli URL interni Docker:

- `http://tesla-api:8000/api/vehicle/poll`
- `http://tesla-api:8000/api/vehicle/latest`
- `http://tesla-api:8000/api/vehicle/command`

## 7. Grafana

Apri `http://localhost:3000`.

Credenziali di default:

- utente: `admin`
- password: `admin`

Il datasource InfluxDB viene creato dal provisioning in `grafana/provisioning`. La dashboard iniziale si trova nella cartella Grafana `Tesla`.

## 8. Spegnimento

```bash
docker compose down
```

I dati persistenti restano in:

- `influxdb-data/`
- `node-red-data/`
- `grafana-data/`
- `tesla-tokens.json`
- `config/`

## Troubleshooting

| Problema | Causa probabile | Soluzione |
|---|---|---|
| `tesla-api` non diventa healthy | proxy o InfluxDB non pronti | `docker compose logs tesla-proxy influxdb tesla-api` |
| Il callback OAuth non arriva | redirect URI non configurata in Tesla | aggiungi `http://localhost:8080/callback` nelle impostazioni app Tesla |
| L'app riparte e richiede sempre OAuth | `TESLA_RESET_TOKEN_ON_START=true` | comportamento voluto; imposta `false` solo se vuoi riusare i token |
| Node-RED non raggiunge l'API | URL host usato dentro container | da Node-RED usa `http://tesla-api:8000`, non `localhost` |
| Grafana non mostra dati | nessun polling ancora eseguito | chiama `POST /api/vehicle/poll` e poi ricarica la dashboard |