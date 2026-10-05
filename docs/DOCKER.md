# Docker: build, rete e servizi

Il container applicativo ora esegue `tesla-api` che espone un servizio API HTTP sulla porta `8000` e usa la porta `8080` per il callback OAuth Tesla.

## Build locale

```bash
docker build -t tesla-fleet-api-service:latest .
```

## Avvio stack completo

```bash
docker compose up --build -d
```

Servizi nel compose:

- `tesla-proxy`
- `tesla-api`
- `influxdb`
- `mosquitto`
- `mysql`
- `node-red`
- `grafana`

Tutti sono collegati a `tesla-network`. I container si chiamano tra loro con i nomi servizio Docker:

- `tesla-api` -> `https://tesla-proxy:4443`
- `tesla-api` -> `http://influxdb:8086`
- `node-red` -> `mosquitto:1883`
- `node-red` -> `mysql:3306`
- `node-red` -> `http://tesla-api:8000`
- `grafana` -> `http://influxdb:8086`

Il proxy viene costruito da `proxy/Dockerfile` usando l'immagine ufficiale Tesla e un healthcheck Go statico. L'immagine ufficiale non contiene `curl`: usarlo nell'healthcheck rende il container `unhealthy` e impedisce l'avvio di `tesla-api`. Il nuovo controllo richiede HTTP 200 da `/health` e verifica la catena TLS contro `config/tls-cert.pem`; non verifica il nome host per supportare i certificati locali autofirmati senza SAN.

Prima dell'avvio prepara `.env` e lancia `bash setup-proxy.sh`, che crea anche il file token vuoto se manca. I bind mount dei file non creano automaticamente directory. Se `tesla-tokens.json` e gia una directory, spostala e riesegui lo script.

`tesla-api` attende proxy e InfluxDB healthy prima di partire. `node-red` attende API, Mosquitto e MySQL healthy; Grafana attende InfluxDB. Queste dipendenze regolano l'avvio, non garantiscono che un servizio venga riavviato se una dipendenza smette di funzionare.

## Variabili principali

Il servizio Go legge `.env` e variabili d'ambiente Docker. Le variabili Docker hanno precedenza.

| Variabile | Default compose | Descrizione |
|---|---|---|
| `TESLA_API_LISTEN_ADDR` | `:8000` | porta HTTP API |
| `TESLA_PROXY_URL` | `https://tesla-proxy:4443` | URL interno del proxy Tesla |
| `TESLA_TOKEN_PATH` | `/app/tesla-tokens.json` | file token montato in volume |
| `TESLA_RESET_TOKEN_ON_START` | `true` | pulisce il token a ogni restart |
| `INFLUX_URL` | `http://influxdb:8086` | URL interno InfluxDB |
| `INFLUX_ORG` | `tesla` | organization InfluxDB |
| `INFLUX_BUCKET` | `vehicle` | bucket InfluxDB |
| `INFLUX_TOKEN` | `${INFLUX_ADMIN_TOKEN:-tesla-dev-token-change-me}` | token API InfluxDB |

Per password e token locali puoi creare variabili shell prima dell'avvio:

```bash
export INFLUX_ADMIN_TOKEN='scegli-un-token-lungo'
export INFLUX_ADMIN_PASSWORD='scegli-una-password'
export GRAFANA_ADMIN_PASSWORD='scegli-una-password'
export MQTT_USERNAME='tesla_nodered'
export MQTT_PASSWORD='scegli-una-password-lunga'
export MYSQL_ROOT_PASSWORD='scegli-un-altra-password-lunga'
export MYSQL_USER='tesla_nodered'
export MYSQL_PASSWORD='scegli-una-password-lunga'
docker compose up --build -d
```

In alternativa, aggiungi queste variabili al tuo `.env` esistente, mantenendo le credenziali Tesla già presenti. L'immagine Node-RED installa `node-red-node-mysql` versione `3.0.4`. I volumi Docker `mosquitto-data` e `mysql-data` conservano rispettivamente messaggi e database.

Le credenziali MySQL, lo schema in `mysql/init` e la configurazione iniziale InfluxDB vengono applicati solo con dati non ancora inizializzati. Cambiare `.env` non aggiorna automaticamente utenti, password o token gia salvati: aggiorna anche il servizio esistente. Non cancellare i dati per applicare nuove credenziali. Per Grafana vedi [la guida dedicata](GRAFANA.md).

Le porte host MQTT `1883` e MySQL `3306` sono pubblicate su tutte le interfacce per l'accesso dalla LAN. MySQL e MQTT richiedono autenticazione; cambia ogni password di esempio e limita gli accessi con il firewall. MQTT senza TLS trasmette credenziali e messaggi in chiaro, quindi non esporre la porta a reti non fidate o a Internet.

## Token Tesla al riavvio

Il compose imposta:

```yaml
TESLA_RESET_TOKEN_ON_START=true
```

Questo comportamento e intenzionale: il servizio svuota `tesla-tokens.json` prima di caricare i token, senza rimuovere il file montato da Docker. Il nuovo OAuth parte alla prima chiamata Tesla, per esempio `POST /api/vehicle/poll`.

`curl http://localhost:8080/callback` non e un test di avvio: il server callback ascolta solo durante OAuth e richiede il codice restituito da Tesla. Per controllare l'API usa `curl http://localhost:8000/health`.

Per conservare i token cambia la voce nell'environment di `tesla-api` in `docker-compose.yml`:

```yaml
environment:
  - TESLA_RESET_TOKEN_ON_START=false
```

Mantieni le altre variabili gia presenti e applica la modifica con `docker compose up -d tesla-api`. Impostare soltanto `.env` o una variabile nella shell non sovrascrive il `true` fissato dal Compose.

## Test container singolo

Per testare solo il servizio API fuori dal compose devi fornire un proxy raggiungibile e InfluxDB raggiungibile:

Prepara prima i file `.env` e `tesla-tokens.json`. Esporta `INFLUX_ADMIN_TOKEN` nella shell: `docker run` non carica automaticamente il valore da `.env` per l'espansione `$INFLUX_ADMIN_TOKEN`. Ferma l'eventuale `tesla-api` del Compose prima di riutilizzare le porte `8000` e `8080`.

```bash
docker run --rm \
  -p 8000:8000 \
  -p 8080:8080 \
  -v "$(pwd)/.env:/app/.env:ro" \
  -v "$(pwd)/tesla-tokens.json:/app/tesla-tokens.json" \
  -e TESLA_PROXY_URL=https://host.docker.internal:4443 \
  -e INFLUX_URL=http://host.docker.internal:8086 \
  -e INFLUX_ORG=tesla \
  -e INFLUX_BUCKET=vehicle \
  -e INFLUX_TOKEN="$INFLUX_ADMIN_TOKEN" \
  tesla-fleet-api-service:latest
```

Su Docker Engine Linux aggiungi `--add-host=host.docker.internal:host-gateway` al comando se quel nome non e risolto automaticamente. Docker Desktop lo fornisce normalmente. Per una prova senza scritture InfluxDB ometti tutte le variabili `INFLUX_*` e assicurati che non siano impostate nel file montato.

## Push su registry

Sostituisci `<registry>` e `<utente>` con i tuoi valori.

```bash
docker tag tesla-fleet-api-service:latest <utente>/tesla-fleet-api-service:latest
docker push <utente>/tesla-fleet-api-service:latest
```

Per GHCR:

```bash
docker tag tesla-fleet-api-service:latest ghcr.io/<utente>/tesla-fleet-api-service:latest
docker push ghcr.io/<utente>/tesla-fleet-api-service:latest
```

## Controlli utili

```bash
docker compose ps
docker compose logs -f tesla-api
docker compose logs -f tesla-proxy
docker compose logs -f mosquitto mysql node-red
curl http://localhost:8000/health
curl -k https://localhost:4443/health
```

Nel JSON di `/health` controlla `proxy` e `influxdb`: HTTP 200 da solo non garantisce dipendenze funzionanti. `curl -k` disabilita la verifica TLS ed e solo una prova locale, non un'impostazione da usare per connessioni non fidate.

## Note di sicurezza

- Non committare `.env`, `tesla-tokens.json`, file `.pem`, `config/` o directory dati locali.
- Cambia `INFLUX_ADMIN_TOKEN`, `INFLUX_ADMIN_PASSWORD` e `GRAFANA_ADMIN_PASSWORD` se esponi i servizi oltre il tuo host.
- Node-RED deve chiamare la nuova API Go; non deve contenere token Tesla.
- Le porte del Compose sono pubblicate su tutte le interfacce. L'API Go non implementa autenticazione dei chiamanti e l'editor Node-RED non e protetto da questa configurazione: limita l'accesso a host e reti fidate o aggiungi autenticazione e un reverse proxy. I token salvati nell'API permettono di inviare comandi al veicolo.