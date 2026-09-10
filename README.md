# Tesla Fleet API service stack

Stack locale per automazioni Tesla con Go, Tesla Vehicle Command proxy, InfluxDB, Node-RED e Grafana.

Il servizio Go `tesla-api` e il punto di integrazione principale: gestisce OAuth Tesla, chiama il proxy Tesla, espone endpoint HTTP per Node-RED e scrive direttamente su InfluxDB.

## Architettura

```text
Node-RED  --->  tesla-api  --->  tesla-proxy  --->  Tesla Fleet API
                    |
                    v
                 InfluxDB  --->  Grafana
```

Tutti i servizi Docker sono collegati alla stessa rete `tesla-network`. Dall'esterno usi lo stesso host/IP con porte diverse.

| Servizio | URL host | Uso |
|---|---|---|
| `tesla-api` | `http://localhost:8000` | API Go per poll dati e comandi |
| `tesla-proxy` | `https://localhost:4443` | proxy Tesla Vehicle Command |
| `influxdb` | `http://localhost:8086` | storico time-series |
| `node-red` | `http://localhost:1880` | automazioni |
| `grafana` | `http://localhost:3000` | dashboard |

## Prerequisiti

- Docker e Docker Compose.
- Tesla Fleet API app creata su developer.tesla.com.
- `TESLA_CLIENT_ID` e `TESLA_CLIENT_SECRET`.
- VIN del veicolo.
- `private-key.pem` nella root del progetto.
- Redirect URI Tesla configurata come `http://localhost:8080/callback`.

## Quick Start

Configura `.env`:

```bash
cp .env.example .env
```

Valori minimi:

```ini
TESLA_CLIENT_ID=your_client_id
TESLA_CLIENT_SECRET=your_client_secret
TESLA_REDIRECT_URI=http://localhost:8080/callback
TESLA_VEHICLE_VIN=your_vehicle_vin
```

Prepara proxy e token file:

```bash
bash setup-proxy.sh
touch tesla-tokens.json
```

Avvia lo stack:

```bash
docker compose up --build -d
```

Controlla l'API:

```bash
curl http://localhost:8000/health
```

La prima chiamata Tesla richiede OAuth. Avvia un poll e poi apri l'URL stampato nei log di `tesla-api`:

```bash
curl -X POST http://localhost:8000/api/vehicle/poll
docker compose logs -f tesla-api
```

## API principali

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

Ultimo snapshot in memoria:

```bash
curl http://localhost:8000/api/vehicle/latest
```

Reference completa: [API.md](API.md).

## Node-RED

Da Node-RED usa gli hostname interni Docker, non `localhost`.

Esempi URL per i nodi HTTP Request:

- `POST http://tesla-api:8000/api/vehicle/poll`
- `GET http://tesla-api:8000/api/vehicle/latest`
- `POST http://tesla-api:8000/api/vehicle/command`

Node-RED orchestra le automazioni, ma non scrive direttamente su InfluxDB e non contiene token Tesla.

Dettagli: [NODE_RED.md](NODE_RED.md).

## InfluxDB e Grafana

Il servizio Go scrive su InfluxDB nelle measurement:

- `tesla_vehicle`
- `tesla_command`

Grafana viene provisionato con un datasource InfluxDB e una dashboard base.

Apri Grafana:

```text
http://localhost:3000
```

Credenziali locali di default:

- utente: `admin`
- password: `admin`

Dettagli: [GRAFANA.md](GRAFANA.md).

## Token Tesla

Il compose imposta:

```ini
TESLA_RESET_TOKEN_ON_START=true
```

Quindi a ogni riavvio `tesla-api` pulisce `tesla-tokens.json` e forza un nuovo OAuth alla prima chiamata Tesla. Questo evita il blocco osservato quando il programma trova un token gia presente.

## File principali

| File | Descrizione |
|---|---|
| [tesla-api.go](tesla-api.go) | servizio Go `tesla-api` |
| [docker-compose.yml](docker-compose.yml) | stack completo |
| [Dockerfile](Dockerfile) | build immagine `tesla-api` |
| [AVVIO.md](AVVIO.md) | guida operativa completa |
| [API.md](API.md) | endpoint e payload |
| [NODE_RED.md](NODE_RED.md) | chiamate da Node-RED |
| [GRAFANA.md](GRAFANA.md) | datasource e dashboard |
| [DOCKER.md](DOCKER.md) | build, rete e registry |

## Sicurezza

Non committare mai:

- `.env`
- `tesla-tokens.json`
- `private-key.pem`
- file `.pem`, `.key`, `.crt`
- `config/`
- `influxdb-data/`
- `node-red-data/`
- `grafana-data/`

Se esponi questi servizi fuori dal tuo host locale, cambia subito `INFLUX_ADMIN_TOKEN`, `INFLUX_ADMIN_PASSWORD` e `GRAFANA_ADMIN_PASSWORD`.

## Verifiche utili

```bash
go test ./...
docker compose config
docker compose build tesla-api
curl http://localhost:8000/health
```

## Riferimenti

- [Tesla Vehicle Command SDK](https://github.com/teslamotors/vehicle-command)
- [Tesla Fleet API Docs](https://developer.tesla.com/docs/fleet-api)
- [Vehicle Command Protocol](https://github.com/teslamotors/vehicle-command/blob/main/README.md)