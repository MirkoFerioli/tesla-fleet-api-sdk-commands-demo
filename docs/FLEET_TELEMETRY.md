# Fleet Telemetry

Integrazione opzionale del server ufficiale Tesla Fleet Telemetry con i servizi gia presenti:

```text
Configurazione: client -> tesla-api -> tesla-proxy (firma esistente) -> Tesla
Dati:          auto -> Fleet Telemetry (mTLS) -> Mosquitto -> Node-RED -> MySQL
```

OAuth, VIN, chiave virtuale e credenziali MQTT vengono riutilizzati. Non vengono create nuove chiavi Fleet o copie dei token. Il polling e i comandi restano invariati; questi dati telemetry non vengono scritti su InfluxDB/Grafana e non aggiornano `/api/vehicle/latest`.

## Prerequisiti

- App Fleet API gia registrata nella regione corretta, chiave pubblica pubblicata sul dominio applicativo e chiave virtuale associata all'auto. Non ripetere questi passaggi se gia completati.
- Veicolo compatibile: Fleet Telemetry richiede almeno firmware 2023.20.6; alcuni Model S/X precedenti non sono supportati. I singoli segnali possono richiedere firmware piu recente.
- FQDN pubblico e porta TCP raggiungibile dall'auto. Puoi preparare i file senza averli ancora scelti, ma non ricevere dati reali.
- Certificato server con SAN del FQDN, chiave TLS corrispondente e catena CA completa. Consigliato un certificato di CA pubblica. Il certificato localhost generato dal setup del proxy NON va riutilizzato.
- Consenso dell'utente, permessi dell'app e piano/fatturazione Fleet API adeguati. Verifica i prezzi correnti Tesla prima di attivare i segnali, soprattutto GPS e velocita.

Con VPS/server pubblico: fai puntare il DNS al server e consenti la porta scelta nel firewall. Con computer locale: serve anche port forwarding del router; sotto CGNAT occorre un endpoint pubblico alternativo. Un load balancer deve inoltrare TCP senza terminare TLS. Un normale reverse proxy HTTPS, CDN o tunnel HTTP non sostituisce il collegamento mTLS diretto.

## 1. Configurazione condivisa

Aggiorna il file `.env` esistente, senza ricopiarlo e senza cambiare le credenziali gia impostate:

```ini
TESLA_TELEMETRY_ENABLED=false
TESLA_TELEMETRY_HOSTNAME=
TESLA_TELEMETRY_PORT=443
TESLA_TELEMETRY_DIR=config/telemetry
TESLA_TELEMETRY_FIELDS=telemetry/vehicle-fields.json
TELEMETRY_UID=1000
TELEMETRY_GID=1000
```

`MQTT_USERNAME`, `MQTT_PASSWORD`, `TESLA_VEHICLE_VIN`, client OAuth, token e `TESLA_PROXY_URL` sono quelli esistenti. La porta esterna configurata viene pubblicata verso `8443` nel ricevitore. Se `443` e gia occupata, scegli un'altra porta pubblica e aggiorna anche firewall/NAT.

Per Docker imposta UID/GID ai valori del proprietario dei file, ottenuti con `id -u` e `id -g`, mai `0`. Il processo ricevitore deve restare nonroot. Il percorso `TESLA_TELEMETRY_DIR` indica la directory sull'host; Compose la rimappa ai percorsi fissi interni dei servizi. Nel container API le variabili di percorso sono impostate da Compose; il profilo segnali e montato da `./telemetry`.

## 2. Preparazione locale senza auto

Dalla root del repository, con Go installato:

```bash
go run . --prepare-telemetry
```

Il comando legge lo stesso `.env`, genera `config/telemetry/server-config.json` con le credenziali MQTT e non chiama Tesla, non richiede OAuth, non tocca token, chiavi o certificati. Dominio e certificati non servono ancora. La directory viene creata con permessi `0700` e il JSON con `0600`. La preparazione non sovrascrive un file esistente: per rigenerarlo dopo un cambio di credenziali, sposta prima consapevolmente il vecchio JSON in un backup privato, poi ripeti il comando. Il backup contiene segreti.

Alternativa senza Go locale, con il setup proxy gia eseguito e il file token presente:

```bash
mkdir -p config/telemetry
chmod 700 config/telemetry
docker compose build tesla-api
docker compose run --rm --no-deps \
  --user "$(id -u):$(id -g)" \
  -v "$PWD/config/telemetry:/app/config/telemetry:rw" \
  tesla-api ./tesla-api --prepare-telemetry
```

Il comando esplicita `./tesla-api` perche l'immagine usa CMD, non ENTRYPOINT. Per un percorso host personalizzato sostituisci anche il volume nel comando. Non avvia il server API ne azzera il token.

Il JSON generato usa `mosquitto:1883`, `topic_base=tesla`, QoS 1 e messaggi non retained. Veicoli, alert, errori e connettivita vengono inviati al broker. Il server ufficiale non espande automaticamente variabili `.env` dentro il JSON: la generazione avviene con un encoder JSON, non con sostituzioni shell.

## 3. Certificati e controllo

Dopo aver scelto il FQDN, imposta `TESLA_TELEMETRY_HOSTNAME`, per esempio sul tuo sottodominio telemetry, e colloca nella directory privata:

| File | Contenuto |
|---|---|
| `fullchain.pem` | Certificato server per il FQDN, seguito dagli intermedi |
| `server-key.pem` | Chiave privata TLS del certificato server |
| `ca.pem` | Certificati CA di fiducia del server, inclusa la radice; inviati all'auto nel campo `ca` |
| `server-config.json` | Configurazione generata, inclusa password MQTT |

Non usare la chiave di firma `config/fleet-key.pem` come chiave TLS. Non impostare `tls.ca_file` del ricevitore con `ca.pem`: quel parametro aggiunge CA per autenticare i client, mentre le CA Tesla di produzione sono gia incluse nel server ufficiale.

```bash
chmod 700 config/telemetry
chmod 600 config/telemetry/*.pem config/telemetry/server-config.json
go run . --check-telemetry > config/telemetry/validate_server.json
chmod 600 config/telemetry/validate_server.json
```

Il controllo verifica hostname, porta, SAN, validita temporale, corrispondenza chiave/certificato e catena CA. Non verifica DNS, firewall, raggiungibilita pubblica o concessioni OAuth. Il JSON prodotto contiene solo hostname, porta e CA pubblica, non password o chiavi.

Senza Go puoi usare lo stesso `docker compose run --rm --no-deps --user "$(id -u):$(id -g)" tesla-api ./tesla-api --check-telemetry`, dopo la build. Con i permessi corretti basta il mount read-only gia definito.

I file devono essere leggibili dall'UID/GID nonroot del ricevitore. Con Podman rootless usa un override Compose locale con `userns_mode: keep-id` sui servizi `fleet-telemetry` e `tesla-api` se la mappatura predefinita impedisce la lettura. Applica lo stesso override ai comandi `run`/`up` e usa UID/GID del proprietario; su SELinux puo servire anche un'etichetta condivisa per i bind mount. Non aggirare il problema con `chmod 777` o esecuzione root. Verifica ownership anche quando passi da Docker a Podman.

## 4. Avvio del ricevitore

```bash
docker compose --profile telemetry up -d fleet-telemetry
docker compose --profile telemetry logs --tail=100 fleet-telemetry
```

Il servizio attende Mosquitto e monta soltanto la directory telemetry in sola lettura. Non riceve token OAuth o la chiave Fleet. L'immagine fissata e `docker.io/tesla/fleet-telemetry:v0.9.5`, distroless: non contiene shell o `curl`. Non ha un healthcheck HTTP fittizio; l'avvio non prova ancora il collegamento dall'auto.

Da una macchina esterna alla rete del server, usa il [validator ufficiale](https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/tools/check_server_cert.sh), esaminandolo prima dell'esecuzione:

```bash
curl --fail --location \
  https://raw.githubusercontent.com/teslamotors/fleet-telemetry/v0.9.5/tools/check_server_cert.sh \
  -o /tmp/tesla-check-server-cert.sh
bash /tmp/tesla-check-server-cert.sh config/telemetry/validate_server.json
```

Il JSON CA/hostname puo essere trasferito alla macchina di verifica; mai trasferire chiave privata o JSON del broker. TLS deve arrivare direttamente al ricevitore. Una normale richiesta HTTP senza certificato client puo essere respinta: e il comportamento previsto per mTLS.

## 5. Consenso GPS e configurazione esplicita dell'auto

Il [profilo esteso](../telemetry/vehicle-fields.json) include `Location`, `VehicleSpeed`, `Gear`, `Odometer`, `BatteryLevel`, `DetailedChargeState`, potenza AC/DC e temperature. Imposta:

```ini
TESLA_TELEMETRY_ENABLED=true
TESLA_OAUTH_SCOPES="openid offline_access vehicle_device_data vehicle_cmds vehicle_charging_cmds vehicle_location"
```

Abilita anche il permesso posizione nella tua app Tesla e ottieni un nuovo consenso utente: un refresh del vecchio token non aggiunge il permesso. Il compose esistente azzera gia i token a ogni avvio dell'API; non cancellare il file montato. Se hai disabilitato questo comportamento, rinnova il consenso consapevolmente prima di procedere.

```bash
docker compose up --build -d tesla-api
curl --max-time 120 -X POST http://localhost:8000/api/vehicle/telemetry/config
```

Alla prima richiesta valida, completa OAuth aprendo l'URL nei log dell'API. Se `curl` scade durante il consenso, ritenta dopo averlo completato. L'API non risveglia esplicitamente il veicolo.

Il POST usa il VIN configurato, hostname/porta/CA locali e il profilo su file. Invia al proxy `POST /api/1/vehicles/fleet_telemetry_config`; il proxy firma con la chiave esistente e inoltra alla variante JWS Tesla. Non devi generare token JWS o nuove chiavi manualmente.

Per scegliere un sottoinsieme o cambiare frequenza:

```bash
curl -X POST http://localhost:8000/api/vehicle/telemetry/config \
  -H 'Content-Type: application/json' \
  -d '{"fields":{"BatteryLevel":{"interval_seconds":60},"Location":{"interval_seconds":30}}}'
```

Il body accetta solo `fields`: non permette hostname, CA, VIN o chiavi arbitrari. Ogni campo deve avere `interval_seconds` intero tra 1 e 86400; sono consentiti da 1 a 100 segnali. Tesla valida anche disponibilita, compatibilita e vincoli propri dei segnali. Per rimuovere il GPS basta omettere `Location` e applicare nuovamente il profilo scelto. Il profilo su file contiene un esempio, non una raccolta attivata all'avvio.

Controlla il risultato completo: HTTP 200 non garantisce l'applicazione se Tesla segnala veicoli saltati o errori nel body.

```bash
curl http://localhost:8000/api/vehicle/telemetry/config
```

Ripeti la lettura a intervalli ragionevoli fino a `synced: true`, con un limite di tentativi. Se non si sincronizza, consulta errori/compatibilita e lo stato dell'auto, senza creare un loop di risvegli. GET e DELETE inoltrano il percorso Tesla per il VIN `/api/1/vehicles/{VIN}/fleet_telemetry_config` e conservano stato HTTP e risposta upstream. Non restituiscono uno stato locale ricostruito.

I segnali sono trasmessi su cambiamento e non piu spesso dell'intervallo scelto: non aspettarti un messaggio ogni 10 secondi a veicolo fermo o addormentato. `DetailedChargeState` richiede firmware 2024.38 o successivo. Verifica unita e supporto nella documentazione Tesla dei [campi disponibili](https://developer.tesla.com/docs/fleet-api/fleet-telemetry/available-data); non assumere che velocita, distanze o temperature seguano le unita del display. Il progetto conserva il valore originale senza conversioni.

## 6. Ricezione MQTT e MySQL

Aggiorna/importa il [flow esistente](../node-red/flows/tesla-mqtt-to-mysql.json) seguendo la [guida Node-RED](NODE_RED.md). Sostituisci il flow precedente, mantenendo una sola sottoscrizione attiva; il volume persistente `/data` non viene sovrascritto automaticamente. Inserisci nelle configurazioni dei nodi le credenziali MQTT/MySQL esistenti e premi Deploy.

Topic pubblicati dal dispatcher ufficiale:

| Topic | Payload JSON |
|---|---|
| `tesla/<VIN>/v/BatteryLevel` | Valore singolo, numero o stringa secondo firmware |
| `tesla/<VIN>/v/Location` | Valore posizione strutturato |
| `tesla/<VIN>/alerts/<nome>/current` | Stato alert |
| `tesla/<VIN>/alerts/<nome>/history` | Storia alert |
| `tesla/<VIN>/errors/<nome>` | Errore |
| `tesla/<VIN>/connectivity` | Stato connessione |

Il VIN e nel topic: non presumere che esista in ogni payload. Ogni segnale e un messaggio distinto, non uno snapshot `vehicle_data`.

Per osservare i messaggi senza mettere password nella cronologia dell'host:

```bash
docker compose exec mosquitto sh -c \
  'mosquitto_sub -h localhost -u "$MQTT_USERNAME" -P "$MQTT_PASSWORD" -t "tesla/#" -q 1 -v'
```

Il tool client passa comunque la password come argomento nel container: usa un ambiente fidato. I payload e i topic possono rivelare VIN e posizione; evita log condivisi.

Prova sintetica del solo percorso broker -> flow -> database, senza chiamare Tesla:

```bash
docker compose exec mosquitto sh -c \
  'mosquitto_pub -h localhost -u "$MQTT_USERNAME" -P "$MQTT_PASSWORD" -t "tesla/TEST/v/BatteryLevel" -q 1 -m "72"'
```

Interroga MySQL usando il prompt password, senza scriverla nel comando:

```bash
docker compose exec mysql sh -c 'exec mysql -u "$MYSQL_USER" -p tesla'
```

```sql
SELECT topic, payload, JSON_TYPE(payload) AS json_type, received_at
FROM mqtt_messages
ORDER BY id DESC LIMIT 20;

SELECT topic, JSON_EXTRACT(payload, '$.latitude') AS latitude,
       JSON_EXTRACT(payload, '$.longitude') AS longitude, received_at
FROM mqtt_messages WHERE topic LIKE 'tesla/%/v/Location'
ORDER BY id DESC LIMIT 20;
```

La forma esatta dei valori segue il dispatcher/firmware; consulta il payload originale se il campo posizione e annidato. Il flow conserva numeri, stringhe numeriche, booleani, oggetti e `null` senza doppia codifica. I payload non JSON sono salvati come stringhe JSON. La tabella attuale viene riutilizzata senza migrazioni. Su un vecchio volume MySQL senza tabella, applica lo script [di inizializzazione](../mysql/init/001-mqtt-messages.sql) al database esistente senza cancellare il volume.

`received_at` e l'ora UTC di ricezione MySQL, non il timestamp del campione originale. QoS 1 puo produrre duplicati. L'ACK affidabile del ricevitore conferma la pubblicazione MQTT, NON il commit MySQL. Il flow non promette consegna offline, deduplicazione o exactly-once; un errore SQL o Node-RED spento puo causare perdita di dati.

## 7. Disattivazione e manutenzione

Prima rimuovi la configurazione dall'auto:

```bash
curl -X DELETE http://localhost:8000/api/vehicle/telemetry/config
curl http://localhost:8000/api/vehicle/telemetry/config
docker compose --profile telemetry stop fleet-telemetry
```

Verifica l'esito Tesla prima di fermare il servizio. Mantieni `TESLA_TELEMETRY_ENABLED=true` finche devi leggere/rimuovere la configurazione: GET/DELETE non richiedono certificati locali validi, ma richiedono OAuth. Poi puoi impostarlo a `false`. Arrestare soltanto il container non cancella la configurazione dell'auto.

Dopo rinnovi TLS riesegui il controllo e riavvia il ricevitore; se cambia la CA di fiducia, aggiorna anche la configurazione dell'auto. Dopo cambi password MQTT rigenera il JSON e riavvia il ricevitore, coordinando le credenziali effettive del broker e dei nodi.

Limita con firewall a reti fidate API, proxy, Node-RED, MQTT, MySQL, InfluxDB e Grafana: il compose esistente pubblica porte sulla LAN e non diventa sicuro per Internet attivando telemetry. Esponi pubblicamente solo la porta mTLS necessaria. Proteggi database e backup, limita gli utenti e definisci una retention per i dati GPS. Una cancellazione MySQL deve essere intenzionale, per esempio con una soglia `received_at` approvata, mai distruggendo i volumi.

## Diagnosi e verifica

- `503` locale: funzionalita disabilitata, profilo non leggibile o certificati mancanti/non validi. `400`: VIN, JSON, segnali/intervalli o scope GPS configurato non validi. Questi errori precedono OAuth.
- Errori TLS: controlla DNS/NAT, porta pubblica, SAN, catena/expiry, permessi dei bind mount e assenza di terminazione HTTPS intermedia.
- Errori upstream/veicoli saltati: conserva il body originale, controlla consenso, chiave associata, regione, firmware e supporto del veicolo. Per diagnosi lato Tesla consulta [Fleet Telemetry](https://developer.tesla.com/docs/fleet-api/fleet-telemetry) e [partner endpoints](https://developer.tesla.com/docs/fleet-api/endpoints/partner-endpoints#fleet-telemetry-errors).
- MQTT presente ma MySQL vuoto: verifica Deploy, credenziali effettive, tabella e nodo Catch/debug Node-RED. Non duplicare il flow durante l'importazione.
- Dati assenti con configurazione sincronizzata: verifica connessione auto/ricevitore e cambiamento effettivo dei segnali; non aumentare il polling per compensare.

Verifiche automatiche del codice: `go test ./...` e `go vet ./...`. Il test della funzione Node-RED usa Node.js locale e viene saltato se non installato. Su Linux puoi eseguire anche `TESLA_TEST_CONTAINERS=1 go test . -run TestTelemetryReceiverContainerSmoke -v`: crea container temporanei con credenziali/certificati sintetici, verifica MQTT autenticato e rifiuto mTLS, poi li rimuove. Usa la rete host ma ascolta solo su porte loopback temporanee, senza lo stack di progetto; non eseguirlo su un host non fidato. Se disponibile usa Podman con `keep-id`, altrimenti Docker. Per verificare anche la preparazione dall'immagine API costruita, passa `TESLA_TEST_API_IMAGE` con il tag locale di quell'immagine.

La verifica pubblica completa richiede validator esterno, `synced: true` e messaggi reali dell'auto nel broker e nel database. La prova sintetica MQTT non dimostra il funzionamento auto -> mTLS.

Fonti: [server ufficiale](https://github.com/teslamotors/fleet-telemetry/tree/v0.9.5), [dispatcher MQTT](https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/datastore/mqtt/README.md), [proxy di firma](https://github.com/teslamotors/vehicle-command/blob/main/pkg/proxy/proxy.go).