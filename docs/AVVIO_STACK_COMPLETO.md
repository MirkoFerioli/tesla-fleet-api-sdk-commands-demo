# Stack completo: cosa fare, punto per punto

Questa e la sequenza per avviare **tutto lo stack con Tailscale**:
API Tesla, proxy di firma, InfluxDB, Grafana, MQTT, MySQL, Node-RED,
Fleet Telemetry, Tailscale e rinnovo automatico dei certificati.

Segui i passi in ordine, un blocco alla volta. Se un comando fallisce,
fermati e risolvi l'errore prima di continuare.

**Non installare Tailscale, Certbot o cert-manager sull'host per questo percorso.**
Tailscale e il rinnovo Let's Encrypt sono nei container. Sull'host servono
Linux, Bash e Docker Compose, oppure Podman con il comando `docker compose`.
La preparazione iniziale del proxy richiede anche OpenSSL.

Questa guida presuppone che l'app Tesla sia gia registrata, la chiave pubblica
pubblicata e la chiave virtuale associata al veicolo. Se i comandi Tesla
funzionavano gia, **non rifare registrazioni e non rigenerare chiavi**.
Per i passaggi con URL `localhost`, usa il browser sul computer che esegue lo
stack; su un server remoto occorre predisporre accessi privati e callback OAuth.

## 1. Apri il terminale nella cartella del progetto

Nel tuo ambiente attuale:

```bash
cd /home/mirko/Progetti_Test/tesla-fleet-api-sdk-commands-demo
```

Se hai distribuito il progetto su un altro computer, usa il percorso di quella
installazione. Esegui i comandi come utente proprietario, **senza `sudo`**.

## 2. Controlla la configurazione esistente

Apri il file delle impostazioni `.env`, senza sostituirlo. Devono essere gia
presenti le tue credenziali Tesla, VIN e password dei servizi:

- `TESLA_CLIENT_ID`, `TESLA_CLIENT_SECRET` e `TESLA_VEHICLE_VIN`.
- `TESLA_REDIRECT_URI`, coerente con l'app Tesla; nella configurazione locale
  standard e `http://localhost:8080/callback`.
- `MQTT_USERNAME` e `MQTT_PASSWORD`.
- `MYSQL_ROOT_PASSWORD`, `MYSQL_USER` e `MYSQL_PASSWORD`.
- `INFLUX_ADMIN_TOKEN`, `INFLUX_ADMIN_PASSWORD` e `GRAFANA_ADMIN_PASSWORD`.

Usa password personali robuste, non i default di sviluppo. Non cambiare alla
cieca password di database gia inizializzati: modificarle nel `.env` non
aggiorna automaticamente gli account salvati nei database.

Lascia inizialmente:

```ini
TESLA_TELEMETRY_ENABLED=false
```

Questo evita di abilitare la gestione della raccolta prima dei controlli.
**Non cancella una configurazione gia inviata all'auto.** Se stai migrando una
raccolta gia attiva, rimuovila prima seguendo la
[procedura di disattivazione](FLEET_TELEMETRY_VPN_LETSENCRYPT.md#8-arresto-e-ripristino).

Non condividere il `.env`, token, password o chiavi private.

## 3. Prepara il proxy solo se non lo avevi gia fatto

Se il proxy funzionava gia e i suoi file sono presenti, salta questo passo.
Altrimenti, con la chiave Fleet esistente disponibile come `private-key.pem`
nella root del progetto:

```bash
bash setup-proxy.sh
```

Questo prepara i file TLS locali del proxy, la copia della chiave di firma e
il file token se manca. Questi certificati locali non sono quelli pubblici
Fleet Telemetry: non copiarli al posto dei certificati Let's Encrypt.

## 4. Prepara Tailscale e il ricevitore

```bash
bash setup-telemetry.sh setup
```

Conferma con `y`. Lo script prepara i file e costruisce le immagini.
Alla prima installazione puo fermarsi chiedendo il login Tailscale. In quel caso:

```bash
bash setup-telemetry.sh login
```

Apri nel browser il link mostrato e accedi all'account Tailscale destinato a
questa installazione. Nel pannello amministrativo Tailscale:

1. Abilita **MagicDNS**.
2. Abilita **HTTPS Certificates**.
3. Autorizza **Funnel** per il nodo appena registrato, secondo la policy della
   tua tailnet. Non aprire indiscriminatamente l'autorizzazione a tutti i nodi.

Poi ripeti:

```bash
bash setup-telemetry.sh setup
```

**Continua solo quando il setup termina correttamente.** Il nome pubblico
`*.ts.net` viene ricavato automaticamente: non devi comprarlo o inventarlo.
Non devi aprire porte sul router per Funnel.

Se lo script segnala certificati gia presenti, non cancellarli alla cieca:
segui la [procedura di migrazione](FLEET_TELEMETRY_VPN_LETSENCRYPT.md#a2-prima-configurazione).
Il setup non invia comandi all'auto e non attiva la raccolta Tesla.

## 5. Avvia tutti i servizi

Nel terminale principale:

```bash
export COMPOSE_FILE=docker-compose.yml:docker-compose.tailscale.yml
export COMPOSE_PROFILES=telemetry
docker compose up --build -d
docker compose ps
```

Questi due `export` selezionano la variante completa con Tailscale. Valgono
solo per il terminale corrente: ripetili quando apri un nuovo terminale per
eseguire altri comandi Compose di questa guida.

Non aggiungere l'override pubblico: e un percorso alternativo, non un pezzo
mancante dello stack Tailscale. Non usare il solo Compose principale per
ricreare il ricevitore gia configurato con Tailscale.

Se un servizio risulta in errore o continua a riavviarsi, controllane i log prima
di proseguire. Tieni privati database, API e pagine di amministrazione: non
attivare una DMZ del router e non esporre le loro porte su Internet.

## 6. Collega Node-RED a MQTT e MySQL

Apri [Node-RED](http://localhost:1880) nel browser.

1. Dal menu scegli **Import** e seleziona il
   [flow predisposto](../node-red/flows/tesla-mqtt-to-mysql.json).
2. Se lo avevi gia importato, aggiorna la copia esistente: mantieni una sola
   copia attiva, per non duplicare i messaggi.
3. Configura il collegamento MQTT con host `mosquitto`, porta `1883` e i valori
   di `MQTT_USERNAME` e `MQTT_PASSWORD` presenti nel `.env`.
4. Configura il collegamento MySQL con host `mysql`, porta `3306`, database
   `tesla` e i valori di `MYSQL_USER` e `MYSQL_PASSWORD`.
5. Premi **Deploy** e controlla che non compaiano errori di collegamento.

Non inserire qui le credenziali dell'account Tesla. Per i dettagli dei nodi
consulta la [guida Node-RED](NODE_RED.md).

## 7. Verifica il ricevitore prima di attivare l'auto

```bash
bash setup-telemetry.sh status
bash setup-telemetry.sh check
```

Il controllo deve riuscire. Verifica poi l'ingresso pubblico **da una connessione
esterna**, seguendo il
[passo A7 della guida VPN](FLEET_TELEMETRY_VPN_LETSENCRYPT.md#a7-prova-da-una-connessione-esterna).
Il controllo locale da solo non prova che l'auto possa raggiungere il servizio.

Se la prova esterna fallisce, fermati. Non disattivare mTLS per far apparire una
pagina nel browser: il ricevitore deve autenticare anche il certificato dell'auto.

## 8. Abilita la raccolta e autorizza Tesla

**Da questo passo puoi attivare una raccolta reale soggetta ai costi Tesla.**
Il profilo attuale include il GPS: verifica consenso, permesso posizione della
tua applicazione Tesla, compatibilita del veicolo e dati che desideri raccogliere.
Se non vuoi il GPS, fai preparare un profilo senza `Location` prima di continuare.

Nel `.env` imposta:

```ini
TESLA_TELEMETRY_ENABLED=true
TESLA_OAUTH_SCOPES="openid offline_access vehicle_device_data vehicle_cmds vehicle_charging_cmds vehicle_location"
```

Nel terminale principale, con le variabili del passo 5 ancora impostate:

```bash
docker compose up --build -d tesla-api
docker compose logs -f tesla-api
```

Lascia aperta questa finestra per leggere i messaggi. Il riavvio API standard
svuota il token conservato: una nuova autorizzazione e prevista.

In un secondo terminale esegui:

```bash
curl --max-time 120 -X POST http://localhost:8000/api/vehicle/telemetry/config
```

**Questo POST configura realmente la raccolta sull'auto con il VIN del progetto.**
Apri l'URL Tesla mostrato nei log, accedi e conferma i permessi richiesti.
Se il comando scade durante il login, completa il consenso e ripeti il POST.
Non ripeterlo continuamente e non creare cicli di risveglio del veicolo.

Controlla la risposta completa: `error` o `skipped_vehicles` richiedono una
verifica anche se la richiesta HTTP sembra riuscita.

## 9. Controlla il risultato

```bash
curl http://localhost:8000/api/vehicle/telemetry/config
```

Attendi che nella risposta compaia:

```json
"synced": true
```

Significa che la configurazione risulta sincronizzata con il veicolo, non ancora
che i messaggi siano stati salvati. Se e `false`, ricontrolla manualmente qualche
volta; se non cambia, verifica gli errori invece di inviare altri POST ripetuti.

Dal terminale principale apri MySQL:

```bash
docker compose exec mysql sh -c 'exec mysql -u "$MYSQL_USER" -p tesla'
```

Inserisci la password MySQL del progetto nel prompt, poi esegui:

```sql
SELECT topic, payload, received_at
FROM mqtt_messages
ORDER BY id DESC LIMIT 10;
```

Devi trovare messaggi recenti del VIN corretto. Digita `exit;` per uscire.
Un'auto addormentata o valori che non cambiano possono non produrre nuovi dati.
Per distinguere ricezione MQTT e salvataggio MySQL consulta il
[passo 11 della guida semplice](FLEET_TELEMETRY_GUIDA_SEMPLICE.md#11-verifica-che-arrivino-e-vengano-salvati-i-dati).

Apri anche [Grafana](http://localhost:3000) con le credenziali configurate.
**Grafana attualmente mostra i dati del polling su InfluxDB, non automaticamente
la telemetria salvata in MySQL.** Avviare tutti i container non crea questa
integrazione. Per provare una singola lettura con scrittura su InfluxDB:

```bash
curl --max-time 120 -X POST http://localhost:8000/api/vehicle/poll
```

Questa e una richiesta aggiuntiva a Tesla, non l'avvio di un polling periodico.

## Avvii successivi

Dopo il primo setup, per avviare nuovamente tutto dalla root del progetto:

```bash
export COMPOSE_FILE=docker-compose.yml:docker-compose.tailscale.yml
export COMPOSE_PROFILES=telemetry
docker compose up -d
```

Non ripetere setup, login Tailscale e POST di configurazione auto a ogni avvio.
Con le impostazioni attuali un riavvio dell'API puo comunque richiedere un nuovo
OAuth alla successiva chiamata Tesla. Il rinnovo dei certificati avviene nei
container: controlla periodicamente `bash setup-telemetry.sh status` e monitora
eventuali servizi `unhealthy`; Docker non invia notifiche automaticamente.

Per fermare temporaneamente tutti i servizi, nello stesso terminale:

```bash
docker compose stop
```

Questo non cancella dati, identita Tailscale o configurazione sull'auto.
Per interrompere definitivamente la raccolta segui prima la
[procedura di disattivazione](FLEET_TELEMETRY_VPN_LETSENCRYPT.md#8-arresto-e-ripristino).
**Non usare `docker compose down -v`: puo cancellare volumi con dati e identita.**
