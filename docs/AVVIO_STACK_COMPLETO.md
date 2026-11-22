# Stack completo: avvio passo per passo

Questa guida avvia API Tesla, proxy di firma, InfluxDB, Grafana, Mosquitto,
MySQL e Node-RED. Esegui i comandi dalla directory principale del progetto.
Per i dettagli consulta la [guida di avvio](AVVIO.md).

Servono Docker Compose, Bash e OpenSSL, un'app Tesla configurata, il VIN e la
chiave privata Fleet in `private-key.pem`. Il callback OAuth locale deve essere
registrato come `http://localhost:8080/callback`.

## 1. Controlla la configurazione

Se `.env` non esiste, crealo dal file di esempio:

```bash
cp .env.example .env
```

Se esiste gia, non sovrascriverlo. Inserisci o verifica:

- `TESLA_CLIENT_ID`, `TESLA_CLIENT_SECRET`, `TESLA_VEHICLE_VIN` e `TESLA_REDIRECT_URI`.
- `MQTT_USERNAME`, `MQTT_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_USER` e `MYSQL_PASSWORD`.
- `INFLUX_ADMIN_TOKEN`, `INFLUX_ADMIN_PASSWORD` e `GRAFANA_ADMIN_PASSWORD`.

Usa password personali robuste e non condividere credenziali, token o chiavi.
Cambiare le password nel file non aggiorna automaticamente database gia
inizializzati. Per i dettagli vedi le [variabili Docker](DOCKER.md#variabili-principali).

## 2. Prepara il proxy

Se il proxy funziona gia e i suoi file di configurazione sono presenti, salta
questo passo. Altrimenti:

```bash
bash setup-proxy.sh
```

Lo script prepara i certificati locali del proxy, copia la chiave privata nella
configurazione e crea il file dei token se manca. Non occorre rigenerare la
chiave Fleet o ripetere la registrazione Tesla se sono gia valide.

## 3. Avvia i servizi

Se nella shell hai impostato `COMPOSE_FILE` o `COMPOSE_PROFILES` per una
configurazione precedente del progetto, rimuovi quei valori prima dell'avvio:

```bash
unset COMPOSE_FILE COMPOSE_PROFILES
```

Per impostazione del Compose, il riavvio di `tesla-api` svuota il file dei token
e richiede una nuova autorizzazione alla prima chiamata Tesla. Per conservarli,
segui le [istruzioni sul reset](API.md#oauth-e-reset-token) prima dell'avvio.

```bash
docker compose up --build -d
docker compose ps
curl http://localhost:8000/health
```

Se un servizio non diventa pronto, controlla i suoi log:

```bash
docker compose logs tesla-proxy influxdb tesla-api
```

Nella risposta di `/health` verifica anche `proxy` e `influxdb`: HTTP 200 da
solo non garantisce che le dipendenze siano funzionanti.

## 4. Autorizza Tesla e prova una lettura

Apri i log in un terminale:

```bash
docker compose logs -f tesla-api
```

Da un altro terminale richiedi una lettura:

```bash
curl --max-time 120 -X POST http://localhost:8000/api/vehicle/poll
```

Apri nel browser l'URL OAuth mostrato nei log e completa l'autorizzazione.
Il browser deve poter raggiungere il callback configurato. Se la richiesta
scade durante il login, ripetila dopo aver completato il consenso.

La lettura puo risvegliare il veicolo e comportare richieste a pagamento o
limiti Fleet API. Una singola richiesta non avvia un polling periodico.

## 5. Configura le automazioni

Apri Node-RED su `http://localhost:11880`. Per archiviare messaggi MQTT,
importa il [flow predisposto](../node-red/flows/tesla-mqtt-to-mysql.json),
configura le credenziali del broker e di MySQL e premi **Deploy**. Serve un
publisher o un'automazione che invii messaggi sui topic `tesla/#`.

Per creare un polling periodico o inviare comandi consulta la
[guida Node-RED](NODE_RED.md). Se il flow MQTT e gia presente, aggiorna la
copia esistente per evitare inserimenti duplicati.

Apri Grafana su `http://localhost:13000`: la dashboard usa i dati del polling
salvati in InfluxDB, non i messaggi MQTT archiviati in MySQL.

| Servizio | Indirizzo sull'host |
|---|---|
| API Tesla | `http://localhost:8000` |
| Proxy Tesla | `https://localhost:4443` |
| InfluxDB | `http://localhost:18086` |
| Mosquitto MQTT | `mqtt://localhost:11883` |
| MySQL | `localhost:13306` |
| Node-RED | `http://localhost:11880` |
| Grafana | `http://localhost:13000` |

Proteggi le interfacce amministrative e limita l'accesso alle porte pubblicate.
MQTT non usa TLS: non esporlo a Internet o a reti non fidate.

## Avvii successivi e arresto

```bash
docker compose up -d
docker compose stop
```

L'arresto non elimina i dati. Non usare `docker compose down -v` se vuoi
conservare i volumi di Mosquitto e MySQL.
