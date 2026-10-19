# Attivare Fleet Telemetry: guida passo per passo

Questa guida ti accompagna nell'attivazione di Fleet Telemetry sul progetto gia configurato. Non devi programmare: alcuni passaggi richiedono di inserire impostazioni o eseguire comandi gia pronti.

**Importante:** la parte che riguarda Internet e i certificati richiede attenzione. Se non hai esperienza con queste impostazioni, falla preparare da una persona tecnica. Non basta accendere il programma sul computer perche l'auto possa raggiungerlo.

## Cosa otterrai

L'auto potra inviare automaticamente informazioni come batteria, ricarica, temperature, velocita e posizione. Il progetto le ricevera e le salvera nel suo archivio MySQL tramite Node-RED, la pagina delle automazioni.

Il profilo gia preparato include la posizione GPS. Questi dati permettono di ricostruire gli spostamenti: attivali solo se desideri raccoglierli e hai il consenso di chi usa il veicolo.

La raccolta non parte da sola installando l'aggiornamento. La attiverai esplicitamente al passo 9. I nuovi dati non appariranno automaticamente nelle dashboard Grafana esistenti.

## 1. Controlla cosa hai gia

Prima di iniziare verifica questi punti:

- Il progetto Tesla e gia configurato con le tue credenziali e il numero identificativo dell'auto, chiamato VIN.
- L'applicazione del progetto e registrata sul [portale sviluppatori Tesla](https://developer.tesla.com/).
- La chiave virtuale del progetto e gia associata all'auto: e l'autorizzazione che permette al progetto di comunicare con il veicolo.
- Docker e installato e il progetto di base si avvia seguendo la [guida di avvio](AVVIO.md).
- Hai accesso all'account Tesla proprietario del veicolo e all'app Tesla sul telefono.
- Hai verificato la compatibilita del veicolo e i costi correnti del servizio Tesla. Alcuni modelli meno recenti non supportano Fleet Telemetry; alcuni dati richiedono aggiornamenti software dell'auto.

Se i comandi del progetto funzionano gia, non ricreare credenziali o chiavi. Se mancano i requisiti Tesla, completa prima la configurazione iniziale con chi ti aiuta: questa guida riutilizza quanto gia presente.

**Puoi continuare quando:** sai che il progetto di base funziona e hai deciso quali dati raccogliere. La trasmissione non e necessariamente gratuita e una raccolta piu frequente puo aumentare i costi.

## 2. Scegli dove lasciare acceso il progetto

L'auto invia i dati attraverso Internet, anche quando non e collegata alla rete di casa. Il computer che li riceve deve quindi essere acceso e raggiungibile.

Puoi usare un server su Internet, oppure un computer di casa configurando anche il router. Alcune connessioni domestiche non permettono di ricevere collegamenti dall'esterno: in quel caso serve un'altra soluzione, da concordare con chi configura la rete.

Chiedi alla persona che ti aiuta di preparare:

| Cosa serve | In parole semplici |
|---|---|
| Un indirizzo pubblico dedicato | Un nome Internet per il ricevitore, per esempio un sottodominio del tuo dominio |
| Una porta raggiungibile | Il punto di ingresso per le connessioni dell'auto; normalmente si usa il numero `443` |
| I certificati di sicurezza | I documenti digitali che permettono all'auto di riconoscere il ricevitore |
| Accessi limitati | Le pagine di gestione e il database devono restare privati |

Puoi dire alla persona tecnica:

> Devo ricevere Tesla Fleet Telemetry. Serve un nome pubblico con certificato corrispondente e un collegamento TCP fino al ricevitore. La connessione sicura deve arrivare direttamente a Fleet Telemetry: non va interrotta da un normale proxy HTTPS. Non esporre a Internet il database, Node-RED o le API di gestione. I dettagli sono nella guida tecnica del progetto.

La [guida tecnica](FLEET_TELEMETRY.md) contiene le indicazioni per rete, certificati e permessi.

**Puoi continuare quando:** hai scelto il nome pubblico e sai chi preparera la connessione. Se non hai ancora deciso, puoi comunque eseguire il passo 4, ma poi devi fermarti prima dell'attivazione.

## 3. Apri il progetto e le sue impostazioni

Apri la cartella del progetto in Visual Studio Code. Dal menu **Terminale**, scegli **Nuovo terminale**: e la finestra in cui inserirai i comandi di questa guida. Devi trovarti nella cartella principale del progetto, quella che contiene [docker-compose.yml](../docker-compose.yml).

I comandi seguenti sono per Linux, l'ambiente attuale del progetto. Esegui un blocco alla volta e aspetta che termini prima di continuare. Se compare un errore, fermati: non proseguire sperando che si risolva nei passi successivi.

Apri il file delle impostazioni esistente, chiamato `.env`. Potrebbe essere nascosto nell'elenco dei file. Non sostituirlo con il [modello di esempio](../.env.example): contiene gia le tue credenziali.

Aggiungi le righe mancanti, oppure aggiorna quelle gia presenti senza duplicarle:

```ini
TESLA_TELEMETRY_ENABLED=false
TESLA_TELEMETRY_HOSTNAME=
TESLA_TELEMETRY_PORT=443
TESLA_TELEMETRY_DIR=config/telemetry
TESLA_TELEMETRY_FIELDS=telemetry/vehicle-fields.json
TELEMETRY_UID=1000
TELEMETRY_GID=1000
```

Lascia inizialmente `false`: significa che la gestione della raccolta non e ancora abilitata. Accanto a `TESLA_TELEMETRY_HOSTNAME=` inserirai il nome pubblico reale, senza `https://`, senza numero di porta e senza barre finali.

Per controllare gli ultimi due numeri, esegui:

```bash
id -u
id -g
```

Il primo risultato va in `TELEMETRY_UID`, il secondo in `TELEMETRY_GID`. Usa `1000` solo se e il numero effettivamente mostrato. Se ottieni `0`, chiedi aiuto prima di continuare: il ricevitore non deve essere eseguito come amministratore.

Non cambiare le credenziali Tesla, MQTT o MySQL gia funzionanti. Non condividere questo file, foto delle password o le chiavi private.

## 4. Prepara i file del ricevitore

Esegui questi comandi nel terminale del progetto:

```bash
mkdir -p config/telemetry
chmod 700 config/telemetry
docker compose build tesla-api
docker compose run --rm --no-deps \
  --user "$(id -u):$(id -g)" \
  -v "$PWD/config/telemetry:/app/config/telemetry:rw" \
  tesla-api ./tesla-api --prepare-telemetry
```

Il primo avvio puo richiedere qualche minuto. Questa operazione prepara il ricevitore usando le password gia impostate: non modifica l'auto e non cancella le autorizzazioni Tesla.

**Risultato atteso:** compare un messaggio che inizia con `Telemetry server config prepared`.

Se il messaggio dice che il file esiste gia, potrebbe essere stato preparato in precedenza. Non cancellarlo alla cieca e non ripetere il comando continuamente: chiedi di verificare se la configurazione esistente e ancora corretta.

## 5. Fai completare la sicurezza della connessione

Questo e il passaggio da affidare a chi configura il server, se non hai esperienza. Nella cartella appena creata devono essere presenti questi file:

| Nome richiesto | A cosa serve |
|---|---|
| `fullchain.pem` | Identifica il ricevitore con il suo nome pubblico |
| `server-key.pem` | E la chiave segreta abbinata al certificato |
| `ca.pem` | Permette all'auto di verificare chi ha emesso il certificato |
| `server-config.json` | E la configurazione gia generata al passo 4 |

I nomi devono essere esattamente quelli indicati. Non basta rinominare un file qualsiasi per renderlo il certificato giusto. I certificati locali del proxy gia presente nel progetto non vanno riutilizzati per questo scopo.

Inserisci nel file `.env` il nome pubblico concordato al passo 2. Se la porta scelta e diversa da `443`, aggiorna anche `TESLA_TELEMETRY_PORT`.

Chiedi di controllare che certificati e cartella siano accessibili al ricevitore, ma non agli altri utenti del computer. Se Docker viene sostituito da Podman, potrebbero servire impostazioni aggiuntive: sono descritte nella guida tecnica. Non risolvere errori di accesso eliminando le protezioni dei file.

Esegui il controllo:

```bash
docker compose run --rm --no-deps \
  --user "$(id -u):$(id -g)" \
  tesla-api ./tesla-api --check-telemetry
```

**Risultato atteso:** viene mostrato un testo con il nome pubblico, la porta e un certificato. Se compare un errore, deve essere risolto prima di continuare. Questo controllo verifica i file, non prova ancora che l'auto possa raggiungere il computer da Internet.

## 6. Accendi il ricevitore e verifica Internet

Avvia i programmi del progetto e il ricevitore aggiuntivo:

```bash
docker compose up --build -d
docker compose --profile telemetry up -d fleet-telemetry
```

La prima riga avvia lo stack abituale. La seconda accende il ricevitore Fleet Telemetry. Nessuna delle due ordina ancora all'auto di trasmettere dati.

Chiedi alla persona tecnica di eseguire anche il controllo Tesla da una connessione esterna alla rete del ricevitore, usando la procedura della [guida tecnica, passo 4](FLEET_TELEMETRY.md#4-avvio-del-ricevitore). Serve a verificare il collegamento che usera l'auto.

Non usare come prova il semplice accesso all'indirizzo dal browser: il ricevitore richiede un'autorizzazione speciale dell'auto e puo rifiutare il browser anche quando funziona correttamente.

**Puoi continuare quando:** il ricevitore e avviato e il controllo esterno e riuscito.

## 7. Prepara il salvataggio dei dati

Apri [Node-RED](http://localhost:1880) nel browser del computer su cui gira il progetto. E la pagina che collega la ricezione dei messaggi al database. Se il progetto gira su un server remoto, chiedi un accesso privato alla pagina: `localhost` indica sempre il computer che stai usando.

1. Apri il menu di Node-RED e scegli **Import**.
2. Seleziona il [flow MQTT verso MySQL](../node-red/flows/tesla-mqtt-to-mysql.json), cioe lo schema di collegamento gia pronto.
3. Se ne avevi gia importato una copia, aggiornala o sostituiscila: non lasciare due copie attive.
4. Apri il nodo di ingresso **Subscribe tesla/#** e la configurazione del collegamento Mosquitto. Inserisci utente e password gia presenti nelle impostazioni del progetto come `MQTT_USERNAME` e `MQTT_PASSWORD`.
5. Apri il nodo **Insert MQTT message** e la configurazione MySQL. Inserisci i valori esistenti di `MYSQL_USER` e `MYSQL_PASSWORD`.
6. Mantieni gli indirizzi gia predisposti: `mosquitto`, porta `1883`; `mysql`, porta `3306`; database `tesla`.
7. Premi **Deploy**, il pulsante che rende operative le modifiche.

Non inserire qui il token o la password dell'account Tesla. Per maggiori dettagli sull'interfaccia consulta la [guida Node-RED](NODE_RED.md).

**Risultato atteso:** non compaiono errori di collegamento. Se il database e molto vecchio e manca la tabella per i messaggi, chiedi di aggiungerla seguendo la guida tecnica, senza cancellare i dati esistenti.

## 8. Consenti anche l'accesso alla posizione

Il profilo fornito raccoglie anche il GPS. Nel portale sviluppatori Tesla, fai verificare che la tua applicazione possa richiedere l'accesso alla posizione.

Nel file `.env`, aggiorna queste impostazioni:

```ini
TESLA_TELEMETRY_ENABLED=true
TESLA_OAUTH_SCOPES="openid offline_access vehicle_device_data vehicle_cmds vehicle_charging_cmds vehicle_location"
```

Le parole nella seconda riga sono i permessi richiesti a Tesla: mantienile come indicate. `vehicle_location` e quello relativo alla posizione.

Riavvia l'API del progetto per leggere le nuove impostazioni:

```bash
docker compose up --build -d tesla-api
```

Nella configurazione standard questo riavvio richiede di autorizzare nuovamente Tesla alla prossima richiesta. E previsto. Se avevi modificato il progetto per conservare sempre l'autorizzazione precedente, chiedi come rinnovare il consenso includendo il GPS. Non cancellare manualmente i file dei token.

Se non vuoi raccogliere la posizione, fermati qui e fai preparare un profilo senza GPS prima del passo successivo: saltare il consenso non modifica automaticamente l'elenco dei dati richiesti.

## 9. Attiva la trasmissione dall'auto

**Da questo passo stai richiedendo a Tesla di applicare la raccolta al veicolo configurato.** Controlla ancora di aver scelto l'auto e i dati corretti.

Apri un secondo terminale in Visual Studio Code, nella stessa cartella del progetto, ed esegui:

```bash
docker compose logs -f tesla-api
```

Questa finestra mostra i messaggi del programma. Nel primo terminale esegui:

```bash
curl --max-time 120 -X POST http://localhost:8000/api/vehicle/telemetry/config
```

Se serve una nuova autorizzazione, nella seconda finestra comparira un indirizzo Tesla da aprire nel browser. Accedi al tuo account e conferma i permessi richiesti, controllando che siano quelli previsti. Se il ricevitore e remoto, fai verificare prima che la pagina di ritorno dell'autorizzazione sia raggiungibile: non cambiare l'indirizzo gia configurato senza aggiornare anche l'applicazione Tesla.

Se il primo comando scade mentre stai ancora autorizzando, completa il consenso e poi ripetilo. Non ripeterlo molte volte in rapida successione.

Leggi la risposta: un messaggio con `error` oppure veicoli indicati in `skipped_vehicles` richiede un controllo, anche se la richiesta sembra essere terminata normalmente. Il programma non risveglia automaticamente l'auto.

## 10. Controlla che l'auto abbia ricevuto le impostazioni

Nel primo terminale esegui:

```bash
curl http://localhost:8000/api/vehicle/telemetry/config
```

Cerca nella risposta:

```json
"synced": true
```

Significa che le impostazioni risultano sincronizzate con l'auto. Se compare `false`, aspetta un po' e ricontrolla manualmente qualche volta. Se non cambia, fermati e chiedi di verificare connessione dell'auto, permessi, compatibilita e risposta Tesla. Non serve inviare comandi di risveglio ripetuti.

Questo controllo non dimostra ancora che i dati siano stati salvati: serve anche il passo successivo.

## 11. Verifica che arrivino e vengano salvati i dati

Per vedere i messaggi ricevuti esegui:

```bash
docker compose exec mosquitto sh -c \
  'mosquitto_sub -h localhost -u "$MQTT_USERNAME" -P "$MQTT_PASSWORD" -t "tesla/#" -q 1 -v'
```

**Risultato atteso:** quando arrivano dati compaiono righe che iniziano con `tesla/`, seguite dal VIN dell'auto e dal tipo di informazione. Non condividere queste righe pubblicamente: possono contenere posizione e altri dati personali. Premi **Ctrl+C** per uscire da questa visualizzazione; non disattiva la raccolta.

I messaggi arrivano quando i dati cambiano, non necessariamente a intervalli fissi. Un'auto ferma o addormentata puo non inviare nuovi valori per un po'. Non guidare usando il terminale: fai controllare i dati da un'altra persona o verifica in seguito.

Per verificare anche il salvataggio, apri il database con questo comando:

```bash
docker compose exec mysql sh -c 'exec mysql -u "$MYSQL_USER" -p tesla'
```

Alla richiesta della password inserisci quella MySQL del progetto, non quella Tesla. E normale che i caratteri digitati non vengano mostrati. Poi esegui:

```sql
SELECT topic, payload, received_at
FROM mqtt_messages
ORDER BY id DESC LIMIT 10;
```

**Risultato atteso:** una tabella con messaggi recenti relativi al VIN corretto. Per uscire digita `exit;` e premi Invio. Se preferisci non usare il database direttamente, chiedi a chi ti aiuta di fare questa verifica.

Se vedi messaggi nel terminale ma non nell'archivio, controlla Node-RED, le sue credenziali e il pulsante Deploy. L'orario salvato e quello di ricezione, non necessariamente quello in cui l'auto ha misurato il dato. Possono esserci messaggi duplicati; se i programmi di salvataggio sono spenti, alcuni dati possono andare persi.

## 12. Come interrompere la raccolta

Non basta chiudere il terminale o spegnere il ricevitore. Prima chiedi a Tesla di rimuovere le impostazioni:

```bash
curl -X DELETE http://localhost:8000/api/vehicle/telemetry/config
curl http://localhost:8000/api/vehicle/telemetry/config
```

Controlla la risposta e fai verificare che la configurazione sia stata rimossa. Se compare un errore, non considerare la raccolta disattivata. Lascia l'impostazione `TESLA_TELEMETRY_ENABLED=true` finche non hai completato questa operazione.

Solo dopo puoi fermare il ricevitore:

```bash
docker compose --profile telemetry stop fleet-telemetry
```

Puoi poi riportare `TESLA_TELEMETRY_ENABLED=false` nelle impostazioni e riavviare l'API come al passo 8. I dati gia salvati restano nel database: interrompere la raccolta non li cancella.

## Se qualcosa non funziona

| Cosa vedi | Cosa fare |
|---|---|
| File gia esistente durante la preparazione | Verifica la configurazione presente senza cancellarla alla cieca |
| Errore su certificato o accesso ai file | Chiedi a chi ha preparato la connessione; non eliminare le protezioni |
| `503` | Controlla se la funzione e abilitata e se sono presenti i file richiesti |
| `400` | Controlla VIN, impostazioni e permesso posizione; conserva il messaggio di errore |
| Richiesta Tesla di nuovo consenso | Completa l'autorizzazione richiesta e verifica i permessi |
| `synced` resta `false` o Tesla segnala errori | Fai verificare permessi, compatibilita e connessione del veicolo |
| Nessun dato dopo la sincronizzazione | Controlla ricevitore e collegamento Internet; considera che i valori vengono inviati quando cambiano |
| Dati ricevuti ma non salvati | Controlla Node-RED e il database |

Per chiedere aiuto indica il numero del passo e il testo dell'errore. Oscura password, token, chiavi private, VIN e posizione prima di condividere schermate o messaggi.

## Dopo l'attivazione

- Lascia accesi ricevitore e programmi di salvataggio quando vuoi raccogliere i dati.
- Controlla periodicamente costi Tesla, spazio disponibile e scadenza dei certificati.
- Concorda per quanto tempo conservare i dati, soprattutto gli spostamenti, e chi puo consultarli.
- Se cambi password, dominio o certificati, fai aggiornare anche la configurazione collegata: non basta modificare un solo file.
- Per cambiare quali dati vengono raccolti o quanto spesso vengono inviati, usa il [profilo dei segnali](../telemetry/vehicle-fields.json) con l'aiuto della [guida tecnica](FLEET_TELEMETRY.md), poi applica nuovamente la configurazione all'auto.

**L'attivazione e verificata quando:** il controllo esterno della connessione e riuscito, l'auto risulta sincronizzata e trovi nel database messaggi reali recenti del veicolo corretto.