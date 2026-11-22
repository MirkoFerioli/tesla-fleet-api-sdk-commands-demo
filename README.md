# Tesla Fleet API service stack

Stack locale per automazioni Tesla con Go, Tesla Vehicle Command proxy, InfluxDB, Mosquitto, MySQL, Node-RED e Grafana. Tutti i servizi si avviano con Docker Compose.

Il servizio Go `tesla-api` e il punto di integrazione principale: gestisce OAuth Tesla, chiama il proxy Tesla, espone endpoint HTTP per Node-RED e scrive direttamente su InfluxDB.

Il flow MQTT verso MySQL puo archiviare messaggi di publisher esterni o automazioni Node-RED; lo stack non include un publisher MQTT per i dati del veicolo.

## Servizi

Docker Compose avvia l'API Tesla, il proxy, InfluxDB, Mosquitto, MySQL, Node-RED e Grafana sulla rete `tesla-network`.

| Servizio | URL host |
|---|---|
| API Tesla | `http://localhost:8000` |
| Callback OAuth | `http://localhost:8080/callback` |
| Proxy Tesla | `https://localhost:4443` |
| Node-RED | `http://localhost:11880` |
| Grafana | `http://localhost:13000` |
| InfluxDB | `http://localhost:18086` |
| MQTT | `mqtt://localhost:11883` |
| MySQL | `localhost:13306` |

Il callback OAuth e disponibile solo durante l'autorizzazione.

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

Se la shell contiene `COMPOSE_FILE` o `COMPOSE_PROFILES` di una configurazione precedente, aggiornali o rimuovili prima dell'avvio.

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