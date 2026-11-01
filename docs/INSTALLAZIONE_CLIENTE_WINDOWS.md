# Installazione presso il cliente (Windows)

Procedura per preparare e installare lo stack Tesla Fleet su un PC Windows del cliente.
La parte di configurazione iniziale va eseguita da chi installa; al cliente vanno consegnati
solo i comandi operativi e le credenziali che gli competono.

## Come funziona

- Sul PC cliente si installa un archivio ZIP con le immagini Docker gia compilate: non serve
  compilare o scaricare immagini durante l'installazione.
- Docker Desktop conserva certificati, token, stato Tailscale e dati applicativi in volumi
  Docker (nella VM Linux di WSL2). Non servono `uid:gid`, symlink o permessi Unix su NTFS.
- L'unico file specifico del cliente da copiare separatamente e `config\fleet-key.pem`, la
  chiave dell'app Tesla **di quel cliente**. Il `.env` con le altre credenziali viene creato
  localmente durante l'installazione.
- Tailscale Funnel ottiene e rinnova il certificato pubblico per l'hostname `*.ts.net`.
  Non occorre configurare un IP pubblico, DNS o porte sul router.
- Il certificato TLS interno del proxy Tesla viene creato al primo avvio e conservato
  nel volume `proxy-tls`.
- L'installazione non attiva la telemetria sull'auto. L'attivazione e un passaggio esplicito.

## 1. Preparazione del pacchetto (sul PC Linux dell'installatore)

Verifica che Docker o Podman sia avviato e che `zip` sia installato. Dalla root del repository:

```bash
bash scripts/crea-pacchetto-cliente.sh 1.0.0
```

Il risultato e `dist/tesla-fleet-1.0.0.zip` (oltre alla directory di staging omonima).
Lo script costruisce immagini `linux/amd64`, le salva in `immagini.tar` e include solo
configurazioni generiche: non include `.env`, chiavi private Tesla o token.

I tag `ghcr.io/mirkoferioli/tesla-*` sono nomi locali delle immagini nel tar: non vengono
pubblicati su GHCR e non richiedono un account GHCR.

### Prerequisiti Tesla del cliente

Prima dell'installazione, procurati quanto segue per ogni cliente:

- App Tesla Developer con Client ID e Client Secret del cliente.
- Redirect URI `http://localhost:8080/callback` registrata per l'app.
- Chiave privata EC P-256 dell'app (`fleet-key.pem`) e chiave pubblica pubblicata nel
  percorso `.well-known/appspecific/com.tesla.3p.public-key.pem` del dominio dell'app.
- Partner registration richiesta da Tesla completata per la regione pertinente.
- VIN del veicolo e accesso del cliente al suo account Tesla.
- Virtual key Tesla associata al veicolo. Se non e stata associata, la procedura e al passo 9.

Proteggi `fleet-key.pem` durante il trasporto: non inserirla nel pacchetto ZIP e non inviarla
via email in chiaro.

## 2. Requisiti del PC Windows

| Requisito | Indicazione |
|---|---|
| Sistema | Windows 10 22H2 o Windows 11, 64 bit x86-64 |
| Windows ARM | Non incluso nel pacchetto attuale |
| RAM | 8 GB minimo, 16 GB consigliati |
| Disco | Almeno 20 GB liberi |
| BIOS/UEFI | Virtualizzazione Intel VT-x o AMD-V attiva |
| Rete | Connessione Internet stabile; nessuna porta in ingresso da aprire |
| Account | Permessi amministrativi per Docker Desktop |

Verifica le condizioni di licenza Docker Desktop per l'uso commerciale del cliente: l'uso
aziendale oltre le soglie previste richiede un abbonamento. Verifica anche il piano e i termini
Tailscale applicabili all'uso commerciale.

## 3. Installare Docker Desktop

1. Installa Docker Desktop per Windows e seleziona il backend WSL2.
2. Riavvia Windows se richiesto e avvia Docker Desktop.
3. In **Settings → General**, attiva **Start Docker Desktop when you sign in to your computer**.
4. Dal Prompt dei comandi verifica:

   ```cmd
   docker version
   docker compose version
   ```

5. In **Impostazioni Windows → Sistema → Alimentazione**, disattiva la sospensione quando il PC
   e collegato all'alimentazione.
6. Configura gli orari attivi di Windows Update per ridurre i riavvii durante gli spostamenti
   dell'auto. Dopo un riavvio il servizio riparte quando Docker Desktop e l'utente Windows sono
   attivi; senza login Windows la telemetria non riparte.

## 4. Estrarre il pacchetto e copiare la chiave

1. Estrai lo ZIP in `C:\TeslaFleet` (evita OneDrive e cartelle sincronizzate).
2. In Esplora file attiva **Visualizza → Estensioni nomi file**.
3. Copia la chiave privata del cliente in:

   ```text
   C:\TeslaFleet\config\fleet-key.pem
   ```

   Il file deve chiamarsi esattamente `fleet-key.pem`, non `fleet-key.pem.txt`.
4. Apri **Prompt dei comandi** e importa le immagini:

   ```cmd
   cd /d C:\TeslaFleet
   tesla.cmd installa
   ```

L'importazione puo richiedere diversi minuti e parecchio spazio disco.

## 5. Configurare l'account Tailscale del cliente (una volta)

Ogni cliente deve usare la propria tailnet e il proprio account Tailscale.

1. Crea o verifica l'account del cliente su [login.tailscale.com](https://login.tailscale.com/).
2. Apri **DNS** nella console amministrativa:
   - attiva **MagicDNS**;
   - attiva **HTTPS Certificates**.
3. In **Access controls**, verifica che la policy consenta Funnel ai membri. Se manca, aggiungi
   il permesso seguente al `nodeAttrs` esistente (non sostituire il resto della policy):

   ```json
   {
     "target": ["autogroup:member"],
     "attr": ["funnel"]
   }
   ```

4. In **Settings → Keys**, genera un'auth key:
   - **Reusable**: disattivato (monouso);
   - **Ephemeral**: disattivato;
   - **Pre-approved**: attivato se la tailnet richiede l'approvazione dei dispositivi.
5. Copia la chiave `tskey-auth-...`; verra inserita nel `.env` al passo successivo.

Non cambiare il nome del nodo `tesla-telemetry` o il nome della tailnet dopo la configurazione.
Il runtime protegge dall'uso silenzioso di un hostname diverso e la modifica richiederebbe
una migrazione esplicita della configurazione dell'auto. L'hostname `*.ts.net` e pubblico e
compare nei log pubblici di Certificate Transparency.

## 6. Creare e compilare `.env`

Dal Prompt dei comandi in `C:\TeslaFleet`:

```cmd
tesla.cmd prepara
```

Lo script crea `.env` da `.env.example`, genera password casuali per i servizi interni e lo apre
con Notepad. Compila questi valori:

| Variabile | Valore |
|---|---|
| `TESLA_CLIENT_ID` | Client ID dell'app Tesla del cliente |
| `TESLA_CLIENT_SECRET` | Client Secret della stessa app |
| `TESLA_VEHICLE_VIN` | VIN del veicolo |
| `TS_AUTHKEY` | Auth key monouso generata al passo 5 |
| `TESLA_TELEMETRY_ENABLED` | Lascia `false` fino al passo 11 |

Salva il file mantenendo il nome `.env`. Evita il carattere `$` nei valori inseriti: Docker
Compose lo interpreta come sintassi di interpolazione. Non modificare le password interne dopo
il primo avvio: i servizi database le usano all'inizializzazione e le conservano nei volumi.

## 7. Primo avvio e certificati Tailscale

```cmd
tesla.cmd avvia
tesla.cmd stato
```

Il primo rilascio del certificato puo richiedere alcuni minuti. Controlla poi:

```cmd
tesla.cmd hostname
tesla.cmd verifica
```

`hostname` deve mostrare un nome `*.ts.net`; `verifica` deve terminare correttamente e creare
`validate_server.json`.

Nella console Tailscale, in **Machines**, apri il nodo `tesla-telemetry` e seleziona
**Disable key expiry**. Se questa opzione resta attiva, la chiave del nodo puo scadere e
interrompere il servizio. Dopo che il nodo e autenticato, puoi rimuovere `TS_AUTHKEY` da `.env`:
lo stato autenticato e persistito nel volume `tailscale-state`.

## 8. Verifica esterna dell'endpoint TLS

Da un PC diverso, fuori dalla rete del cliente e senza Tailscale collegato, esegui con OpenSSL:

```bash
H=tesla-telemetry.<tailnet>.ts.net
openssl s_client -connect "$H:443" -servername "$H" \
  -verify_hostname "$H" -verify_return_error </dev/null
```

Sostituisci `<tailnet>` con il nome effettivo mostrato dalla console. L'obiettivo e
`Verify return code: 0 (ok)`. Un alert TLS `certificate required` puo essere normale: il
receiver richiede il certificato client del veicolo. Se disponibile, esegui anche lo script
di verifica Tesla `check_server_cert.sh` usando `validate_server.json`.

## 9. Login Tesla e autorizzazioni

```cmd
tesla.cmd login-tesla
```

Il comando apre una seconda finestra con i log e avvia il primo poll. Apri nel browser l'URL
OAuth Tesla che compare nei log, sullo stesso PC Windows, e accedi con l'account Tesla del
cliente. Il callback usa `localhost:8080`; completa l'autorizzazione entro cinque minuti.

I token vengono salvati nel volume `tesla-data` e non vengono cancellati ai riavvii. Se Tesla
lo richiede, associa la virtual key dall'app Tesla o visita `https://tesla.com/_ak/<dominio-app>`
dal telefono del cliente e completa la procedura mostrata da Tesla.

## 10. Importare il flow Node-RED

1. Apri [http://localhost:11880](http://localhost:11880) e scegli **Menu → Import**.
2. Importa `C:\TeslaFleet\node-red\tesla-mqtt-to-mysql.json`.
3. Configura le credenziali dei nodi:
   - MQTT: broker `mosquitto`, porta `1883`, username/password `MQTT_USERNAME` e
     `MQTT_PASSWORD` dal `.env`;
   - MySQL: host `mysql`, porta `3306`, database `tesla`, username/password `MYSQL_USER` e
     `MYSQL_PASSWORD` dal `.env`.
4. Seleziona **Deploy**.

Per ulteriori dettagli vedi [NODE_RED.md](./NODE_RED.md).

## 11. Attivare la telemetria sull'auto

Attiva la telemetria solo dopo che i certificati sono validi, la verifica esterna e riuscita,
il login Tesla e completato e la virtual key e associata.

1. Nel `.env`, cambia:

   ```ini
   TESLA_TELEMETRY_ENABLED=true
   ```

2. Dal Prompt dei comandi:

   ```cmd
   tesla.cmd avvia
   tesla.cmd attiva-telemetria
   tesla.cmd stato-telemetria
   ```

Il POST senza payload usa il profilo `telemetry/vehicle-fields.json`, che include il segnale
`Location` e richiede lo scope `vehicle_location` e il consenso Tesla corrispondente. Ottieni il
consenso informato del cliente per la raccolta dei dati (inclusa la posizione) e verifica i
costi e le condizioni Fleet API prima dell'attivazione.

Per disattivare l'invio dall'auto, usa `tesla.cmd disattiva-telemetria` prima di fermare o
rimuovere lo stack.

## 12. Test dopo il riavvio

Prima di lasciare il cliente, riavvia Windows e accedi all'account. Dopo alcuni minuti:

```cmd
tesla.cmd stato
tesla.cmd verifica
```

Apri [Grafana](http://localhost:13000) e verifica che arrivino dati. Spiega al cliente che il PC
deve restare acceso, connesso a Internet e con Docker Desktop attivo.

## 13. Aggiornare le immagini

1. Estrai il nuovo pacchetto in una cartella temporanea.
2. Copia `immagini.tar`, `docker-compose.yml` e `tesla.cmd` nella cartella cliente. Non
   sovrascrivere `.env`, `config\fleet-key.pem` o i volumi Docker.
3. Esegui:

   ```cmd
   tesla.cmd installa
   ```

4. Aggiorna `TESLA_VERSION` in `.env` con la versione del nuovo pacchetto.
5. Avvia e controlla:

   ```cmd
   tesla.cmd avvia
   tesla.cmd verifica
   ```

I volumi mantengono token, certificati, stato Tailscale e database. Non usare
`docker compose down -v` durante un aggiornamento.

## 14. Backup e ripristino

- Proteggi e salva `.env` e `config\fleet-key.pem` in un archivio cifrato: sono credenziali
  specifiche del cliente.
- Per esportare il database MySQL:

  ```cmd
  tesla.cmd backup-db
  ```

- I certificati del proxy e della telemetria vengono rigenerati/rinnovati dai servizi. Lo stato
  Tailscale e i dati storici invece risiedono nei volumi: per un backup completo usa procedure
  Docker Desktop che preservino i volumi, oppure esegui un dump applicativo prima di migrare il
  PC.

Non distribuire mai un `.env` o un backup contenente credenziali a un altro cliente.

## 15. Disinstallazione

1. Se la telemetria e attiva, esegui prima `tesla.cmd disattiva-telemetria` e verifica il
   risultato. Questo rimuove la configurazione telemetrica dal veicolo.
2. Per arrestare i container senza cancellare i dati:

   ```cmd
   tesla.cmd ferma
   ```

3. Per rimuovere anche tutti i dati e i volumi, da `C:\TeslaFleet` esegui
   `docker compose down -v`. **Questa operazione e irreversibile.**
4. Rimuovi il nodo `tesla-telemetry` dalla console Tailscale; revoca l'accesso dell'app Tesla e
   rimuovi la virtual key dal veicolo secondo la procedura Tesla.
5. Disinstalla Docker Desktop e rimuovi la cartella di installazione, dopo aver verificato
   eventuali backup da conservare.

## 16. Rifare l'autenticazione Tailscale

Se e stato autenticato l'account sbagliato, esegui questa procedura solo se la telemetria non e
ancora attiva sull'auto. Se e attiva, disattivala prima con `tesla.cmd disattiva-telemetria`.

```cmd
tesla.cmd ferma
docker volume rm tesla-fleet_tailscale-state tesla-fleet_telemetry-certs
```

Genera una nuova auth key dalla tailnet corretta, inseriscila in `.env` e riparti dal passo 7.
La rimozione di `telemetry-certs` elimina anche i certificati correnti.

## 17. Diagnostica

| Sintomo | Controllo o soluzione |
|---|---|
| `required variable ... is missing a value` | Verifica che il file si chiami `.env` (non `.env.txt`) e che le variabili richieste siano compilate |
| Errore bind su `fleet-key.pem` | Verifica il file `config\fleet-key.pem` e il nome esatto, comprese le estensioni |
| `No such image` | Esegui `tesla.cmd installa`; verifica che `TESLA_VERSION` corrisponda al pacchetto importato |
| Tailscale mostra un URL di login | L'auth key manca, e stata gia usata o il dispositivo richiede approvazione; completa il login/approvazione dalla console del cliente |
| Worker segnala HTTPS non abilitato | Attiva **HTTPS Certificates** nella console DNS Tailscale, quindi guarda `tesla.cmd log telemetry-certificates` |
| Endpoint esterno non raggiungibile | Verifica permesso Funnel in Access controls e usa `docker compose exec tailscale tailscale funnel status` |
| Worker segnala `hostname changed` | Il nome macchina o la tailnet sono cambiati; ripristina il nome originale o migra esplicitamente la configurazione del veicolo |
| `tesla.cmd verifica` fallisce | Controlla `tesla.cmd log telemetry-certificates` e `tesla.cmd log fleet-telemetry`; il worker riprova il rinnovo |
| API risponde `503` sulla configurazione | Verifica `TESLA_TELEMETRY_ENABLED=true`, poi esegui `tesla.cmd avvia` |
| Login Tesla richiesto dopo un riavvio | Verifica che `TESLA_RESET_TOKEN_ON_START` sia `false` nel compose e che il volume `tesla-data` non sia stato rimosso |
| Servizio fermo dopo reboot | Docker Desktop richiede che l'utente Windows abbia effettuato l'accesso; verifica l'avvio automatico |
| `port is already allocated` | Un altro programma occupa `8000`, `8080`, `11880` o `13000`; libera la porta prima dell'avvio |
| Errore WSL/virtualizzazione | Attiva VT-x/AMD-V nel BIOS/UEFI, abilita WSL2 e riavvia Windows |
| Il sistema sta per usare `docker compose down -v` | Fermati e verifica il backup: `-v` cancella i volumi e i dati persistenti |
