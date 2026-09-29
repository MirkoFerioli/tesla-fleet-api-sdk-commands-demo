# Tesla Fleet API service stack

Stack locale per automazioni Tesla con Go, Tesla Vehicle Command proxy, InfluxDB, Mosquitto, MySQL, Node-RED e Grafana. Tutti i servizi si avviano con Docker Compose.

Il servizio Go `tesla-api` e il punto di integrazione principale: gestisce OAuth Tesla, chiama il proxy Tesla, espone endpoint HTTP per Node-RED e scrive direttamente su InfluxDB.

## Servizi

Docker Compose avvia l'API Tesla, il proxy, InfluxDB, Mosquitto, MySQL, Node-RED e Grafana sulla rete `tesla-network`.

| Servizio | URL host |
|---|---|
| API Tesla | `http://localhost:8000` |
| Node-RED | `http://localhost:1880` |
| Grafana | `http://localhost:3000` |
| InfluxDB | `http://localhost:8086` |
| MQTT | `mqtt://localhost:1883` |
| MySQL | `localhost:3306` |

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

Modifica `.env` con le credenziali Tesla e imposta password robuste per Mosquitto e MySQL. Non avviare lo stack con i valori segnaposto.

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

## Documentazione

Consulta l'[indice delle guide](docs/README.md) per configurazione, API, avvio, Docker Compose, Node-RED e Grafana.

Le porte MQTT e MySQL sono pubblicate sulla LAN. Usa password robuste, limita l'accesso con il firewall e non esporre MQTT senza TLS a reti non fidate o a Internet.