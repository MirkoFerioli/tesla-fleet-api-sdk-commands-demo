# Node-RED: configurare endpoint, nodi e payload

Questa guida spiega come chiamare il servizio `tesla-api` da Node-RED e
correggere le configurazioni mostrate nelle immagini degli errori. Gli esempi
principali assumono che Node-RED e API siano nello stesso stack Compose.

## 1. Cosa correggere nelle immagini

| Configurazione mostrata | Correzione |
|---|---|
| GET verso `http://localhost:8000/...` | Nel container Node-RED, `localhost` indica Node-RED stesso. Usa `http://tesla-api:8000/...`. |
| POST verso `localhost:8000/api/vehicle/command` | Inserisci un URL completo, con `http://`, e il nome del servizio API. |
| `command`, `param` e `wake` nella sezione Headers | Questi valori appartengono al corpo JSON della richiesta, non agli header. Il nome corretto e `params`, al plurale. |
| Inject con il solo timestamp prima del POST | Sostituisci `msg.payload` con un oggetto JSON, oppure inserisci un nodo Change/Function che lo costruisca. |
| Return impostato su `a UTF-8 string` | Scegli `a parsed JSON object` per usare direttamente le proprieta della risposta. |

`ECONNREFUSED` indica che non e stata stabilita la connessione TCP: non e
un errore di validazione del comando Tesla. Nel caso dello stack Compose,
l'indirizzo `localhost` e il primo elemento da correggere; se l'errore
persiste, verifica anche che l'API sia avviata e in ascolto.

Correggere l'indirizzo non basta per il POST: occorre correggere anche il
payload, altrimenti la richiesta puo raggiungere l'API ma ricevere HTTP 400.

## 2. Quale indirizzo usare

| Da dove parte la richiesta | Base URL dell'API |
|---|---|
| Nodo HTTP Request nel container Node-RED dello stack | `http://tesla-api:8000` |
| Terminale sul computer che pubblica la porta API | `http://localhost:8000` |
| Node-RED installato direttamente sullo stesso computer dell'API | `http://localhost:8000` |
| Node-RED su un altro computer | `http://<IP-del-computer-API>:8000`, se raggiungibile e consentito dal firewall |

Il nome `tesla-api` e risolvibile tra i container della rete `tesla-network`;
non e un nome DNS da usare nel browser o in un container su una rete diversa.
La porta `8000` deve essere raggiungibile dal processo Node-RED, non soltanto
dal browser dell'utente. Con Docker Desktop o WSL, verifica dove viene
eseguito quel processo prima di scegliere l'indirizzo.

Per aprire l'editor Node-RED dello stack nel browser usa:

```text
http://localhost:11880
```

`11880` e la porta dell'editor sull'host, non la porta dell'API. La porta
`8080` serve invece al callback OAuth e non va usata per questi endpoint.
Non esporre l'API o l'editor direttamente a Internet per risolvere un problema
di collegamento.

## 3. Quali nodi usare

I nodi seguenti sono inclusi in Node-RED: non servono palette Tesla aggiuntive.

| Nodo | Funzione |
|---|---|
| **Inject** | Avvia una richiesta manuale e prepara `msg.payload`. Per un POST seleziona il tipo JSON, non timestamp. |
| **Change** | Facoltativo: imposta `msg.payload` come oggetto JSON senza scrivere JavaScript. |
| **Function** | Alternativa al Change: costruisce payload e header tramite JavaScript. |
| **HTTP Request** | Invia la richiesta all'API Go. E il nodo corretto per chiamare gli endpoint. |
| **Debug** | Mostra la risposta. Durante la configurazione seleziona l'intero messaggio, `complete msg object`. |
| **Catch** | Facoltativo: intercetta gli errori dei nodi selezionati; collegalo a un Debug. |

**HTTP In** e **HTTP Response** servono a creare un endpoint gestito da
Node-RED: non servono per chiamare un endpoint del servizio Go.

Per le prove iniziali disabilita negli Inject sia la ripetizione sia l'invio
automatico all'avvio. Dopo ogni modifica premi **Done**, poi **Deploy**.

## 4. Prima prova: GET /health

Crea questo flow:

```text
Inject -> HTTP Request -> Debug
```

Nel nodo HTTP Request imposta:

| Campo | Valore |
|---|---|
| Method | `GET` |
| URL | `http://tesla-api:8000/health` |
| Payload, se presente nell'editor | `Ignore` |
| Return | `a parsed JSON object` |
| Use authentication | Disabilitato |
| Use custom SSL/TLS configuration | Disabilitato, l'API interna usa HTTP |
| Use proxy | Disabilitato, salvo un proxy di rete richiesto dalla tua installazione |
| Headers | Nessuno necessario |

Per questo GET l'Inject puo contenere un timestamp: il payload viene ignorato.
Premi il pulsante dell'Inject e verifica nel Debug:

- `msg.statusCode`: deve essere il numero `200`.
- `msg.payload.status`: deve essere `"ok"`.
- `msg.payload.proxy` e `msg.payload.influxdb`: controlla anche questi campi.

HTTP 200 da `/health` conferma che l'API ha risposto, ma non garantisce che
proxy e InfluxDB siano funzionanti. Questo endpoint non invia comandi al
veicolo e non avvia OAuth.

Se non ricevi risposta, dal terminale nella root del progetto esegui:

```bash
docker compose ps
docker compose logs --tail=100 tesla-api tesla-proxy influxdb
docker compose exec node-red wget -qO- http://tesla-api:8000/health
```

L'ultimo comando verifica la raggiungibilita dalla stessa rete del nodo
HTTP Request. Se fallisce anche quello, il problema non e nel payload.
Non condividere log contenenti token, credenziali o dati personali del veicolo.

## 5. GET: leggere i dati del veicolo

Usa lo stesso flow della prova health e configura HTTP Request:

| Campo | Valore |
|---|---|
| Method | `GET` |
| URL | `http://tesla-api:8000/api/vehicle/data?endpoints=charge_state` |
| Payload | `Ignore`, se disponibile |
| Return | `a parsed JSON object` |
| Headers | Nessuno necessario |

`endpoints` e un parametro della query URL, non un header. Per piu sezioni:

```text
http://tesla-api:8000/api/vehicle/data?endpoints=charge_state,drive_state,climate_state,vehicle_state
```

Esempio indicativo di risposta HTTP 200:

```json
{
  "data": {
    "charge_state": {
      "battery_level": 80
    }
  }
}
```

Il livello di carica, se presente nella risposta, si trova in
`msg.payload.data.charge_state.battery_level`. Questo GET non scrive su
InfluxDB e non aggiorna lo snapshot di `/latest`.

## 6. POST: polling e salvataggio su InfluxDB

Crea un nuovo flow `Inject -> HTTP Request -> Debug`.

Nell'Inject imposta la proprieta **msg.payload**, scegli il tipo **JSON** e
inserisci questo oggetto:

```json
{
  "endpoints": "charge_state,drive_state,climate_state,vehicle_state"
}
```

Nel nodo HTTP Request:

| Campo | Valore |
|---|---|
| Method | `POST` |
| URL | `http://tesla-api:8000/api/vehicle/poll` |
| Payload, se disponibile | Invia `msg.payload` come corpo; non scegliere `Ignore` |
| Return | `a parsed JSON object` |
| Headers | `Content-Type` = `application/json`, valore di tipo stringa |

HTTP Request serializza l'oggetto in `msg.payload` come JSON. Non aggiungere
virgolette intorno all'intero oggetto e non inviare il timestamp dell'Inject.

La risposta contiene `status`, `timestamp` e `written`, non i dati completi
del veicolo. Per leggerli dopo il poll usa `GET /api/vehicle/latest`.
Verifica che InfluxDB sia configurato: `written: true` da solo non prova una
scrittura se l'API e stata avviata senza una configurazione InfluxDB completa.

## 7. POST: inviare un comando

**Attenzione: premere questo Inject invia un comando reale al veicolo.**
Usa prima `/health` per verificare il collegamento. Non impostare ripetizioni,
invio all'avvio o retry automatici sui comandi durante la configurazione.

Nell'Inject seleziona **msg.payload** di tipo **JSON**:

```json
{
  "command": "door_lock",
  "params": {},
  "wake": true
}
```

Nel nodo HTTP Request:

| Campo | Valore |
|---|---|
| Method | `POST` |
| URL | `http://tesla-api:8000/api/vehicle/command` |
| Payload, se disponibile | Invia `msg.payload` come corpo |
| Return | `a parsed JSON object` |
| Headers | Solo `Content-Type` = `application/json` |

Elimina dagli header le righe `command`, `param`, `params` e `wake`.
Non aggiungere un header `Authorization` con token Tesla: OAuth e gestito
dal servizio Go, non da questo flow.

### Tipi e nomi corretti

| Campo del corpo | Tipo | Esempio |
|---|---|---|
| `command` | Stringa | `"door_lock"` |
| `params` | Oggetto JSON | `{}` oppure `{"on": true}` |
| `wake` | Booleano, facoltativo | `true`, non `"true"` |

`params` non e `param`, non e una stringa vuota e non e `"{}"`.
Il campo `wake` e accettato per compatibilita, ma il servizio attuale lo
ignora: anche con `false` controlla il veicolo e lo risveglia se necessario.

Altri payload, da usare solo quando desideri realmente eseguire l'azione:

```json
{"command": "door_unlock", "params": {}}
```

```json
{"command": "set_sentry_mode", "params": {"on": true}}
```

```json
{"command": "set_sentry_mode", "params": {"on": false}}
```

Controlla `msg.statusCode` e il contenuto di `msg.payload.response`.
HTTP 200 non garantisce che Tesla abbia eseguito l'azione: una risposta
puo contenere `result: false`. Il servizio incapsula il corpo restituito
dal proxy; con una risposta Tesla del tipo `{"response":{"result":true}}`,
il risultato si trova in `msg.payload.response.response.result`.
Espandi il Debug per verificare la struttura effettivamente ricevuta.

## 8. Alternativa: costruire il corpo con Function

Se vuoi mantenere l'Inject timestamp mostrato nelle immagini, aggiungi una
Function prima di HTTP Request:

```text
Inject -> Function -> HTTP Request -> Debug
```

Per un comando di blocco porte, il contenuto della Function puo essere:

```javascript
msg.headers = {
    "Content-Type": "application/json"
};
msg.payload = {
    command: "door_lock",
    params: {},
    wake: true
};
return msg;
```

In questa variante la Function imposta l'header: non occorre duplicarlo
nella tabella Headers del nodo HTTP Request. Metodo, URL e Return restano
quelli del paragrafo sul comando. Usa questa variante **oppure** l'Inject
JSON, non due preparazioni diverse che sovrascrivono il payload.

Un nodo Change puo svolgere lo stesso compito impostando `msg.payload`
a un valore di tipo JSON; l'header puo restare configurato in HTTP Request.

`msg` e l'intero messaggio Node-RED, mentre `msg.payload` e il corpo da
inviare. Dopo HTTP Request, `msg.payload` contiene invece la risposta e
`msg.statusCode` contiene lo stato HTTP, oppure un codice di errore di rete.
Se riusi il messaggio per un'altra richiesta, prepara nuovamente payload
e header: non riutilizzare alla cieca gli header della risposta precedente.

## 9. Ultimo snapshot, OAuth e tempi di risposta

Per lo snapshot dell'ultimo poll riuscito usa:

```text
GET http://tesla-api:8000/api/vehicle/latest
```

Configura il nodo come gli altri GET. Se non esiste un poll riuscito dall'avvio
dell'API, ricevi HTTP 404 con `no vehicle data has been polled yet`.
Questo endpoint non legge nuovi `vehicle_data`, ma controlla comunque lo
stato del veicolo e puo risvegliarlo.

La prima chiamata a un endpoint del veicolo puo richiedere OAuth:

```bash
docker compose logs -f tesla-api
```

Apri l'URL di autorizzazione mostrato nei log dal browser che puo raggiungere
il callback configurato. Non inserire client secret o token Tesla nei nodi.
Il riavvio dell'API, con il Compose attuale, svuota il token e puo richiedere
una nuova autorizzazione.

Il risveglio puo richiedere fino a 75 secondi; OAuth e le altre operazioni
si aggiungono a questa attesa. Il server API ha un write timeout di due
minuti. Se il client termina la prima lettura durante OAuth, completa il
login e ripeti manualmente la lettura. Un timeout su un comando non prova
che l'azione non sia stata eseguita: verifica il risultato prima di ripeterlo.
Le chiamate periodiche, anche a `/latest`, possono mantenere il veicolo
sveglio, consumare batteria e comportare costi o limiti Fleet API.

## 10. Interpretare gli errori

| Risultato nel Debug | Verifica |
|---|---|
| `ECONNREFUSED` | Host/porta corretti dal processo Node-RED; API avviata e in ascolto. Prova `/health` dal container. |
| `ENOTFOUND` | Nome `tesla-api` scritto correttamente e container sulla stessa rete Compose. |
| HTTP 400, `invalid JSON body` | Inject/Change/Function deve produrre il corpo JSON corretto, non timestamp o testo non JSON. |
| HTTP 400, errore su `command` | Il comando deve essere nel corpo e avere un nome valido; non negli header. |
| HTTP 404 | Controlla il percorso. Per `/latest`, leggi il corpo: puo semplicemente mancare il primo poll. |
| HTTP 405 | Metodo errato: `data` e `latest` sono GET; `poll` e `command` sono POST. |
| HTTP 502 | Leggi `msg.payload.error` e i log API: OAuth, configurazione VIN, proxy, risveglio, Tesla o scrittura InfluxDB. |
| Risposta mostrata come stringa | Imposta Return su `a parsed JSON object`, oppure usa un nodo JSON dopo HTTP Request, non entrambe le conversioni. |
| HTTP 200 ma comando non riuscito | Controlla il risultato Tesla nel corpo, non soltanto lo stato HTTP. |

Per il debug iniziale lascia disattivata l'opzione **Only send non-2xx
responses to Catch node**, cosi il Debug dopo HTTP Request mostra anche
le risposte HTTP di errore. Un Catch collegato a un altro Debug aiuta invece
a vedere gli errori di trasporto; non usarlo per ritentare comandi alla cieca.

## Checklist finale

- L'editor dello stack e su `http://localhost:11880`.
- Nel container, le richieste usano `http://tesla-api:8000`.
- `/health` risponde prima di provare endpoint del veicolo.
- I POST inviano un oggetto JSON in `msg.payload`.
- Negli header non ci sono `command`, `params` o `wake`.
- Return e impostato su oggetto JSON e Debug mostra l'intero messaggio.
- Dopo le modifiche e stato premuto Deploy.
- Le prove dei comandi sono manuali, senza ripetizione o invio all'avvio.

Riferimenti: [contratto API](API.md), [guida Node-RED](NODE_RED.md) e
[messaggi e oggetti nella documentazione ufficiale Node-RED](https://nodered.org/docs/user-guide/messages).
