# Avvio stack Tesla API, InfluxDB, Node-RED e Grafana

Questa guida avvia tutto lo stack Docker:

- `tesla-proxy`: proxy ufficiale Tesla Vehicle Command, usato per firmare e inoltrare le chiamate.
- `tesla-api`: servizio Go HTTP che legge la Tesla API, invia comandi generici e scrive direttamente su InfluxDB.
- `influxdb`: database time-series per storico veicolo e audit comandi.
- `node-red`: automazioni che chiamano la nuova API Go.
- `mosquitto`: broker MQTT con autenticazione.
- `mysql`: database per i messaggi MQTT raccolti da Node-RED.
- `grafana`: dashboard su InfluxDB.

Tutti i container stanno sulla stessa rete Docker `tesla-network`. Dall'esterno usi lo stesso IP del computer e porte diverse.

## 1. Prerequisiti

- Docker e Docker Compose.
- Bash e OpenSSL per eseguire `setup-proxy.sh` (su Windows usa Git Bash con OpenSSL disponibile).
- `TESLA_CLIENT_ID` e `TESLA_CLIENT_SECRET` da developer.tesla.com.
- VIN del veicolo.
- Chiave privata Fleet EC P-256 in `private-key.pem` nella root del progetto, distinta dalla chiave TLS del proxy.
- Redirect URI Tesla configurata come `http://localhost:8080/callback`.

## 2. Configurazione Tesla

```bash
cp .env.example .env
```

Se `.env` esiste gia, aggiornalo senza sovrascrivere le credenziali presenti.

Modifica `.env`:

```ini
TESLA_CLIENT_ID=xxx
TESLA_CLIENT_SECRET=xxx
TESLA_REDIRECT_URI=http://localhost:8080/callback
TESLA_VEHICLE_VIN=xxx
```

Sostituisci anche i placeholder MQTT e MySQL presenti nel file copiato:

```ini
MQTT_USERNAME=tesla_nodered
MQTT_PASSWORD=<password-MQTT-unica>
MYSQL_ROOT_PASSWORD=<password-root-MySQL-unica>
MYSQL_USER=tesla_nodered
MYSQL_PASSWORD=<password-MySQL-unica>
```

`MQTT_USERNAME`, `MQTT_PASSWORD`, `MYSQL_ROOT_PASSWORD` e `MYSQL_PASSWORD` sono obbligatorie per Compose; i placeholder del file di esempio non sono password sicure. Imposta anche `INFLUX_ADMIN_TOKEN`, `INFLUX_ADMIN_PASSWORD` e `GRAFANA_ADMIN_PASSWORD` con valori personali: i default del Compose sono solo per sviluppo locale. Vedi [variabili Docker](DOCKER.md#variabili-principali).

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

Con `TESLA_RESET_TOKEN_ON_START=true`, a ogni riavvio il servizio Go svuota `tesla-tokens.json` senza rimuovere il file montato. Questo forza un nuovo OAuth; un token gia presente non e di per se un errore. Per riusare i token imposta `TESLA_RESET_TOKEN_ON_START=false` nella sezione `environment` di `tesla-api` in `docker-compose.yml`, poi ricrea il servizio. La sola modifica di `.env` non basta, perche il Compose imposta esplicitamente `true`.

## 3. Preparazione proxy

```bash
bash setup-proxy.sh
```

`setup-proxy.sh` crea:

- `config/tls-cert.pem`
- `config/tls-key.pem`
- `config/fleet-key.pem`
- `tesla-tokens.json` vuoto, solo se manca

Se `tesla-tokens.json` e una directory creata da un precedente avvio Docker, spostala prima di eseguire lo script. Il token deve essere un file, non una directory.

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

La richiesta puo restare in attesa dell'autorizzazione. Da un secondo terminale guarda i log:

```bash
docker compose logs -f tesla-api
```

Apri nel browser l'URL Tesla stampato nei log. Dopo il login Tesla, il callback torna su `http://localhost:8080/callback` e il servizio salva il nuovo `tesla-tokens.json`.

Con questo redirect, apri il browser sul computer che esegue Docker: `localhost` si riferisce al computer del browser. Se la prima richiesta termina per timeout, ripetila dopo aver completato l'autorizzazione.

## 6. Chiamate principali

Health check:

```bash
curl http://localhost:8000/health
```

Controlla anche i campi `proxy` e `influxdb` nel JSON: l'API restituisce HTTP 200 e `status: ok` anche quando una dipendenza segnala un errore.

La porta `8080` ascolta soltanto durante il flusso OAuth. Non usare `curl http://localhost:8080/callback` per verificare l'avvio: senza autenticazione in corso puo restituire `Empty reply from server`.

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

MQTT e MySQL conservano i dati nei volumi Docker `mosquitto-data` e `mysql-data`. Non usare `docker compose down -v` se vuoi mantenerli.

## Troubleshooting

| Problema | Causa probabile | Soluzione |
|---|---|---|
| `tesla-http-proxy` unhealthy con `curl` non trovato | healthcheck incompatibile con l'immagine minimale ufficiale | usa il compose aggiornato e `docker compose up --build -d` |
| `failed to reset Tesla token file` con `device or resource busy` | tentativo di rimuovere il file bind-mounted | ricostruisci `tesla-api`: il reset aggiornato svuota il file senza rimuoverlo |
| `tesla-tokens.json` e una directory o manca | Docker ha creato una directory al posto del file | sposta la directory e lancia `bash setup-proxy.sh` |
| `tesla-api` non parte o non diventa healthy | dipendenze non pronte, file montati errati o errore del processo Go | `docker compose logs tesla-proxy influxdb tesla-api`; controlla anche i campi di `/health` |
| Il callback OAuth non arriva | redirect URI non configurata in Tesla | aggiungi `http://localhost:8080/callback` nelle impostazioni app Tesla |
| L'app riparte e richiede sempre OAuth | `TESLA_RESET_TOKEN_ON_START=true` | imposta `false` nell'environment del servizio in `docker-compose.yml` e ricrealo con `docker compose up -d tesla-api` |
| Node-RED non raggiunge l'API | URL host usato dentro container | da Node-RED usa `http://tesla-api:8000`, non `localhost` |
| Grafana non mostra dati | nessun polling ancora eseguito | chiama `POST /api/vehicle/poll` e poi ricarica la dashboard |