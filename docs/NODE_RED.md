# Node-RED

Node-RED deve orchestrare automazioni chiamando `tesla-api`. Non deve scrivere su InfluxDB e non deve contenere token Tesla.

Lo stack include anche Mosquitto e MySQL. Il [flow importabile](../node-red/flows/tesla-mqtt-to-mysql.json) salva i messaggi JSON ricevuti sui topic `tesla/#` nella tabella `tesla.mqtt_messages`.

### Importazione flow MQTT → MySQL

1. Avvia lo stack dopo aver impostato le credenziali MQTT e MySQL nel file `.env`.
2. Apri `http://localhost:1880` e importa il file [tesla-mqtt-to-mysql.json](../node-red/flows/tesla-mqtt-to-mysql.json) dal menu Import.
3. Apri la configurazione del broker Mosquitto e inserisci `MQTT_USERNAME` e `MQTT_PASSWORD`.
4. Apri la configurazione MySQL e inserisci `MYSQL_USER` e `MYSQL_PASSWORD`. Host, porta e database sono già impostati su `mysql:3306` e `tesla`.
5. Distribuisci il flow. Ogni messaggio JSON su `tesla/#` viene aggiunto a `mqtt_messages`; il database assegna l'ora UTC di ricezione.

La palette `node-red-node-mysql` è installata nell'immagine Node-RED ed è fissata alla versione `3.0.4`. Le credenziali non sono incluse nel flow esportato.

Il flow non viene importato automaticamente e lo stack non pubblica dati Tesla su MQTT: devi collegare un publisher o un'automazione MQTT. La funzione del flow serializza `msg.payload` con `JSON.stringify`; se ricevi JSON come stringa e vuoi salvarlo come oggetto, inserisci un nodo JSON prima della funzione di insert. Un JSON pubblicato come oggetto viene salvato come oggetto, una stringa come stringa JSON.

La tabella viene creata dagli script `mysql/init` solo alla prima inizializzazione del volume MySQL. Se riusi un database gia esistente senza `mqtt_messages`, applica lo script SQL anche a quel database. Le credenziali inserite nei nodi devono corrispondere agli utenti effettivamente presenti, non solo ai nuovi valori di `.env`.

### Endpoint interni

| Servizio | Endpoint dai container | Porta host |
|---|---|---|
| Mosquitto MQTT | `mosquitto:1883` | `1883` |
| MySQL | `mysql:3306` | `3306` |

Le porte host sono pubblicate su tutte le interfacce per consentire connessioni dalla LAN. MQTT su porta `1883` non cifra il traffico: usa credenziali robuste e limita l'accesso con il firewall a una rete fidata. Non esporre questi servizi direttamente a Internet.

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

Non usare polling troppo frequente: aumenta le richieste Fleet API e puo comportare costi e limiti di utilizzo. Il servizio non esegue un risveglio esplicito per il polling; le richieste possono fallire se il veicolo dorme. Evita di aggiungere un risveglio automatico a ogni polling se non necessario, per limitare il consumo di batteria.

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

Controlla anche `proxy` e `influxdb` nel JSON: la sola risposta HTTP 200 verifica la raggiungibilita dell'API, non la salute delle dipendenze. La prima richiesta Tesla puo richiedere OAuth: autorizza manualmente dai log prima di attivare le automazioni periodiche.
