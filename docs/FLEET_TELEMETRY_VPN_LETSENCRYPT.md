# Fleet Telemetry con VPN e Let's Encrypt

Guida per questo progetto, verificata sulle fonti indicate il 20 ottobre 2026.
I comandi sono da eseguire consapevolmente sulla tua infrastruttura: la scrittura
di questa guida non installa servizi, non apre porte e non configura l'auto.

## 1. Serve un ingresso pubblico

Puoi tenere il ricevitore a casa, anche sotto CGNAT, usando una VPN insieme a
un ingresso pubblico TCP e a un certificato Let's Encrypt.

**Una VPN privata da sola non basta:** l'auto non entra nella tua VPN. Deve
raggiungere un nome pubblico attraverso Internet. Un normale abbonamento VPN
per navigare non garantisce questo ingresso.

Le alternative sono:

| Soluzione | Dominio e certificato | Quando sceglierla |
| --- | --- | --- |
| Tailscale Funnel in modalita TCP | Nome del nodo `*.ts.net`, certificato Let's Encrypt ottenuto con `tailscale cert` | Prima prova senza IP pubblico domestico e senza acquistare un dominio telemetry |
| VPS pubblico + VPN WireGuard + proxy TCP | Tuo dominio, certificato Let's Encrypt tramite Certbot | Maggiore controllo di dominio, traffico e infrastruttura |
| Router con IP pubblico + inoltro TCP | Tuo dominio, certificato Let's Encrypt tramite Certbot | Se non sei sotto CGNAT e puoi aprire una porta |

Per una prima installazione domestica, il percorso A e quello piu semplice da
provare. Funnel introduce dipendenza dal servizio Tailscale, dalle sue condizioni
e dai suoi limiti correnti: prima di un uso continuativo verifica disponibilita,
costi, limiti e funzionamento con il tuo veicolo. Non e una certificazione Tesla
di compatibilita del servizio.

La regola
Tesla confermata e: **mTLS deve terminare sul servizio Fleet Telemetry**. [1], [2]

## 2. Il vincolo da non sbagliare: mTLS

Il certificato server permette all'auto di riconoscere il ricevitore. Il
certificato client Tesla permette al ricevitore di riconoscere il veicolo.
Il secondo controllo non viene sostituito da Let's Encrypt. [1], [3]

Il collegamento deve essere:

```text
Auto -> ingresso pubblico TCP -> eventuale VPN -> Fleet Telemetry
        <--------- stessa sessione TLS/mTLS fino al ricevitore --------->
```

Per questo non usare un normale reverse proxy HTTPS che termina TLS, una CDN
HTTP o il proxy `tesla-proxy` del progetto come ingresso telemetry.
Non usare neppure `--tls-terminated-tcp` di Tailscale. Il proxy di firma sulla
porta `4443` serve ai comandi Fleet API, non ai dati inviati dall'auto. [1], [4]

Questa guida riutilizza OAuth, VIN, chiave virtuale e credenziali gia esistenti.
Non rigenerare la chiave Fleet e non cambiare il dominio applicativo Tesla solo
per aggiungere il ricevitore. Il nome TLS del ricevitore e un'impostazione
distinta dal sito che pubblica la chiave dell'applicazione.

## 3. Prima di iniziare

Servono un host Linux sempre acceso, il progetto di base funzionante, account
Tesla e veicolo compatibile, un account Tailscale e Docker Compose (oppure
Podman con provider Compose). Nel percorso A sull'host bastano Bash e il motore
container: non installare Tailscale, Go, Certbot o cert-manager. Per le prove
esterne servono `curl`, `openssl` e `jq` sulla macchina di verifica.

Esegui i comandi locali dalla root del progetto, in una shell Bash, un blocco
alla volta. I nomi `example.com` e `nome-tailnet.ts.net` sono segnaposto.
Non pubblicare password, token, VIN, chiavi private o dati GPS.

Qui `config/telemetry` e il percorso standard. Se hai personalizzato
`TESLA_TELEMETRY_DIR`, adatta tutti i percorsi, inclusi script e volumi.

**Sicurezza:** il Compose attuale pubblica anche servizi di gestione sulla rete.
Non attivare una DMZ del router e non inoltrare tutte le porte. Mantieni privati
`8000`, `8080`, `4443`, `1880`, `1883`, `3306`, `8086` e `3000`. Per i servizi
usati solo localmente puoi sostituire le rispettive pubblicazioni con binding
`127.0.0.1`, per esempio `127.0.0.1:1880:1880`. Valuta separatamente il callback
OAuth `8080`: il browser che autorizza deve continuare a raggiungerlo.
Docker puo aggirare alcune regole UFW: non affidarti al solo firewall senza
verificare binding e accessibilita da un altro host.

## 4. Percorso A: Tailscale Funnel e certificato Let's Encrypt

### A1. Il pacchetto Compose

Usa [docker-compose.tailscale.yml](../docker-compose.tailscale.yml) insieme al
Compose principale. Non aggiungere l'override pubblico in questo percorso.
Il pacchetto comprende Tailscale userspace, un worker certificati e il ricevitore
ufficiale con un piccolo supervisore Go. Non richiede container privilegiati,
`/dev/net/tun`, rete host o socket Docker/Podman.

```text
Auto -> nome-reale.ts.net:443 -> Funnel TCP
     -> container tailscale -> fleet-telemetry:8443 sulla rete Compose
     -> mTLS termina nel ricevitore -> MQTT -> Node-RED -> MySQL
```

Non viene pubblicata alcuna porta host telemetry: `127.0.0.1` nel container
Tailscale non e il ricevitore, che si raggiunge con il nome Compose.
Il worker usa soltanto il socket Tailscale e la directory certificati. Il socket
permette di amministrare quel nodo: non condividerlo con altri servizi.
Il worker opera come root nel container con le sole capability necessarie a
gestire i proprietari dei file; Fleet Telemetry resta nonroot.

### A2. Prima configurazione

Mantieni il `.env` del progetto gia configurato. Il file token e i certificati
del proxy devono gia esistere come richiesto dal setup di base. Imposta
`TESLA_TELEMETRY_ENABLED=false` se stai preparando o migrando il ricevitore.
Questo non cancella una configurazione gia presente sull'auto: prima di una
migrazione segui la procedura di disattivazione nella sezione 8.

Come utente proprietario del progetto, senza `sudo`:

```bash
bash setup-telemetry.sh dry-run
bash setup-telemetry.sh setup
```

Lo [script](../setup-telemetry.sh) chiede conferma, costruisce le immagini,
prepara directory private e configurazione Funnel, imposta UID/GID e la porta
pubblica `443`. Prima di aggiornare le sole impostazioni telemetry conserva
una copia privata del `.env` in `config/telemetry/env-before-setup`; non cambia
credenziali Tesla/MQTT, chiavi Fleet o token OAuth. Non riavvia l'API.

La directory puo essere personalizzata nel `.env` con `TESLA_TELEMETRY_DIR`:
lo script accetta un percorso letterale relativo al progetto, senza spazi,
espansioni, commenti sulla stessa riga o collegamenti simbolici. Rifiuta
impostazioni telemetry duplicate. La cartella del progetto puo contenere spazi.
Su Podman configura `keep-id`; su host SELinux va verificata anche l'etichetta
dei bind mount condivisi. Non disabilitare le protezioni per aggirare un errore.

Se esistono gia `fullchain.pem`, `server-key.pem` o `ca.pem` tradizionali, il
setup si ferma anziche sovrascriverli. Dopo aver rimosso la configurazione
dall'auto e fermato il vecchio ricevitore, sposta esplicitamente quei tre file
in un backup privato, poi ripeti il setup. Non eliminare `server-config.json`.

### A3. Collega questa installazione a Tailscale

Al primo avvio il setup puo terminare con codice `2`: significa che devi ancora
autenticare il nodo o abilitare HTTPS. Non e richiesta una password in chat.

```bash
bash setup-telemetry.sh login
```

Apri il link Tailscale nel browser e completa l'accesso con l'account destinato
a questa installazione. Nel pannello amministrativo abilita MagicDNS e HTTPS
Certificates, e autorizza Funnel per il solo nodo interessato secondo la policy
della tailnet. Controlla anche la scadenza dell'identita del nodo. [5], [6]

Ripeti quindi:

```bash
bash setup-telemetry.sh setup
```

Il nome reale `*.ts.net` viene ricavato dal nodo, non da un segnaposto; eventuali
suffissi assegnati da Tailscale vengono rispettati. Non usare l'IP `100.x.y.z`
e non aggiungere CNAME personalizzati. I nomi dei certificati sono pubblici nei
registri Certificate Transparency: evita dati personali nel nome macchina.

### A4. Certificati e avvio automatici

Il worker richiede i PEM con `tailscale cert`, verifica chiave, hostname,
scadenza e catena, scarica le radici X1/X2 dalle URL HTTPS ufficiali Let's Encrypt
e pubblica solo una generazione completa. L'API riutilizza i collegamenti
`fullchain.pem`, `server-key.pem` e `ca.pem` previsti dal progetto. [7], [8]

`ca.pem` contiene solo CA ed e destinato al campo `ca` dell'auto. Non impostare
`tls.ca_file` del ricevitore con questo file: le CA Tesla per autenticare i
**client** sono gia incorporate nel server ufficiale. [1], [3]

Lo script crea il JSON MQTT soltanto se manca, esegue `--check-telemetry` e
avvia worker e ricevitore. Non attiva mai la trasmissione dall'auto. Se la prima
emissione supera tre minuti, verifica i log e ripeti il setup dopo aver risolto
la causa. Non eseguire piu worker sulla stessa directory.

### A5. Stato e controlli locali

```bash
bash setup-telemetry.sh status
bash setup-telemetry.sh check
docker compose -f docker-compose.yml -f docker-compose.tailscale.yml \
  --profile telemetry logs --tail=100 telemetry-certificates fleet-telemetry
```

`status` mostra anche ultimo controllo riuscito, scadenza e ultimo errore.
`check` verifica salute del worker e file locali; non dimostra che il servizio
sia raggiungibile dall'auto. Per produrre il JSON pubblico del validatore:

```bash
umask 077
docker compose -f docker-compose.yml -f docker-compose.tailscale.yml \
  run --rm --no-deps --user "$(id -u):$(id -g)" \
  tesla-api ./tesla-api --check-telemetry > config/telemetry/validate_server.json
```

Adatta il percorso del file se hai personalizzato la directory. Fermati sugli
errori: non avviare l'auto con un JSON vuoto o un container in riavvio continuo.

### A6. Persistenza e ridistribuzione

Per i successivi avvii, senza ripetere setup o cambiare l'autorizzazione Tesla:

```bash
docker compose -f docker-compose.yml -f docker-compose.tailscale.yml \
  --profile telemetry up -d tailscale telemetry-certificates fleet-telemetry
```

Usa sempre entrambi i file Compose anche per gestire questo ricevitore.
Il volume `tailscale-state` conserva l'identita; `tailscale-socket` e soltanto
il canale locale di amministrazione. Lo stato TLS e in `config/telemetry`.
L'host deve riavviare il proprio motore container; non serve un timer certificati.

Distribuisci sorgenti, Dockerfile, Compose e script, **non** il tuo `.env`,
cartelle `config`, volumi, token o certificati privati. Ogni destinatario deve
registrare il proprio nodo e autorizzare la propria auto. Non clonare un'identita
Tailscale fra installazioni, non condividere auth key del produttore.
Funnel dipende dai servizi esterni e dalle loro condizioni correnti: verifica
limiti, costi e adeguatezza prima di adottarlo come servizio continuativo.

### A7. Prova da una connessione esterna

Da un altro computer, preferibilmente non nella tailnet e non nella LAN:

```bash
TELEMETRY_HOST='ricevitore.nome-tailnet.ts.net'
openssl s_client -connect "$TELEMETRY_HOST:443" \
  -servername "$TELEMETRY_HOST" -verify_hostname "$TELEMETRY_HOST" \
  -verify_return_error -showcerts </dev/null
```

Devi raggiungere il ricevitore e vedere il certificato con nome e catena
corretti. Un errore finale come `certificate required` puo essere normale:
non stai presentando il certificato client Tesla. Non disattivare mTLS per
far apparire una pagina nel browser. [1], [3]

Esegui anche il validatore Tesla. Trasferisci alla macchina esterna **solo**
`validate_server.json`, non chiave privata o `server-config.json` con password.
Scarica lo script, leggilo prima di eseguirlo e usa il JSON trasferito:

```bash
curl --fail --location \
  https://raw.githubusercontent.com/teslamotors/fleet-telemetry/v0.9.5/tools/check_server_cert.sh \
  -o /tmp/tesla-check-server-cert.sh
bash /tmp/tesla-check-server-cert.sh ./validate_server.json
```

Il validatore richiede i suoi strumenti locali, fra cui `jq` e `openssl`.
Non prova una sessione applicativa autenticata con l'auto e non sostituisce
il controllo hostname sopra o la ricezione di dati reali. [9]

### A8. Attiva l'auto solo dopo le verifiche

Riprendi i passi 7-11 della [guida semplice](FLEET_TELEMETRY_GUIDA_SEMPLICE.md):
configura Node-RED, controlla scope e consenso GPS, poi abilita telemetry.

Nel `.env` imposta `TESLA_TELEMETRY_ENABLED=true` e gli scope necessari come
documentato li. Quindi:

```bash
docker compose -f docker-compose.yml -f docker-compose.tailscale.yml up --build -d tesla-api
docker compose -f docker-compose.yml -f docker-compose.tailscale.yml logs --tail=100 tesla-api
curl --max-time 120 -X POST http://localhost:8000/api/vehicle/telemetry/config
curl http://localhost:8000/api/vehicle/telemetry/config
```

Il riavvio API standard azzera l'autorizzazione conservata: completa il nuovo
OAuth quando richiesto. Il POST modifica la configurazione del veicolo e puo
avviare una raccolta soggetta a costi Tesla. Controlla la risposta intera,
inclusi errori e `skipped_vehicles`, e poi attendi `synced: true` con letture
manuali ragionevoli. Non creare cicli di wake-up.

La prova finale e vedere messaggi reali del VIN corretto in MQTT e MySQL.
Il solo `synced: true` non prova che il tunnel riceva i dati. I dati GPS sono
personali e i messaggi non arrivano necessariamente a intervalli fissi.

## 5. Rinnovo automatico e scelta del gestore

**Non installare cert-manager in questo Compose:** e un controller Kubernetes,
non un servizio autonomo per Docker Compose. Avrebbe senso se il progetto fosse
distribuito su Kubernetes. Per un nome `ts.net`, Tailscale gestisce gia ACME con
Let's Encrypt; per il dominio proprio del percorso B si usa invece Certbot
DNS-01. Non sommare due sistemi di emissione per lo stesso nome.
Fonte: [cert-manager ufficiale](https://github.com/cert-manager/cert-manager#readme).

Il puro Funnel TCP non rinnova i PEM esportati per un altro processo. [7], [10]
Nel pacchetto questo compito e svolto da `telemetry-certificates`:

- Controlla all'avvio e ogni 24 ore; rinnova quando restano al massimo 14 giorni.
- In caso di errore ritenta dopo 5 minuti senza sostituire i file attivi.
- Valida la coppia completa prima di spostare atomicamente il collegamento
  `certificates/current` su una directory `release-*` privata.
- Mantiene la CA gia pubblicata se valida il nuovo certificato; un errore
  `CA update required` blocca la rotazione e richiede un intervento esplicito.
- Il supervisore del ricevitore rileva la nuova generazione entro circa 5 secondi
  e riavvia soltanto il proprio processo figlio. Tailscale, API e database non
  vengono riavviati. Le connessioni auto si interrompono durante questo passaggio.

Le versioni precedenti restano disponibili per il ripristino e contengono chiavi
private: proteggile e definisci una retention. Non modificare singoli PEM nella
generazione attiva. Non vengono inviati POST automatici alla configurazione auto.

Lo stato salute diventa negativo su errore del worker, controllo riuscito piu
vecchio di 48 ore o certificato con meno di un giorno residuo. **Configura un
monitoraggio esterno:** Docker non invia notifiche da solo e `unhealthy` non
garantisce un riavvio automatico. Usa `bash setup-telemetry.sh status` e i log
per identificare il problema. La salute del worker non e un test dei dati auto.

Per ripristinare una generazione precedente, ferma worker e ricevitore, verifica
che quella coppia sia ancora valida e usa un collegamento relativo `current`
verso la directory `release-*` scelta. Poi esegui il controllo API e riavvia
entrambi usando l'override Tailscale. Prima di una modifica alla CA destinata
all'auto concorda la migrazione e riapplica esplicitamente la configurazione
Tesla; un semplice riavvio non aggiorna la fiducia del veicolo.

## 6. Percorso B: dominio proprio, VPS e VPN

Questa alternativa non usa Funnel. E adatta se vuoi `telemetry.tuodominio.it`
o controllare direttamente l'ingresso pubblico. Richiede amministrazione del
VPS e della VPN; non e sufficiente acquistare un servizio VPN commerciale.

```text
Auto -> telemetry.tuodominio.it:443 -> VPS con HAProxy in TCP
     -> WireGuard -> 10.77.0.2:8443 sul server domestico
     -> container Fleet Telemetry:8443, terminazione mTLS
```

### B1. Prepara il VPS e WireGuard

**Passo 1.** Procurati un VPS Linux con IP pubblico e controlla i costi.

**Passo 2.** Crea un record DNS `A` per `telemetry.tuodominio.it` verso l'IP del
VPS. Pubblica `AAAA` solo se il percorso IPv6 e effettivamente funzionante.
Se usi Cloudflare DNS, lascia il record in modalita **DNS only**, senza proxy
HTTP/CDN.

**Passo 3.** Installa WireGuard su VPS e host domestico tramite i pacchetti della
distro. Genera una coppia di chiavi distinta su ciascun host e scambia solo le
pubbliche. Non riutilizzare chiavi Fleet o TLS.

```bash
umask 077
wg genkey | tee wireguard-private.key | wg pubkey > wireguard-public.key
```

**Passo 4.** Con l'amministratore crea `/etc/wireguard/wg0.conf`, protetto `0600`,
con questi modelli, sostituendo tutti i segnaposto. I file di chiave non vengono
letti automaticamente dal campo `PrivateKey`: inserisci li il contenuto corretto
tramite un editor fidato.

Sul VPS:

```ini
[Interface]
Address = 10.77.0.1/24
ListenPort = 51820
PrivateKey = CHIAVE_PRIVATA_VPS

[Peer]
PublicKey = CHIAVE_PUBBLICA_CASA
AllowedIPs = 10.77.0.2/32
```

Sull'host domestico:

```ini
[Interface]
Address = 10.77.0.2/24
PrivateKey = CHIAVE_PRIVATA_CASA

[Peer]
PublicKey = CHIAVE_PUBBLICA_VPS
Endpoint = IP_PUBBLICO_VPS:51820
AllowedIPs = 10.77.0.1/32
PersistentKeepalive = 25
```

**Passo 5.** Prima di attivare il firewall mantieni accesso SSH amministrativo.
Sul VPS consenti TCP `443`, UDP `51820` e SSH dalle sole reti autorizzate.
L'host domestico avvia la connessione UDP verso il VPS, anche dietro CGNAT;
`PersistentKeepalive` mantiene la mappatura NAT. Non serve una route
`0.0.0.0/0`: non stiamo spostando tutto il traffico nella VPN. [11]

**Passo 6.** Su entrambi gli host, dopo la configurazione:

```bash
sudo systemctl enable --now wg-quick@wg0
sudo wg show wg0
```

Controlla handshake recente e traffico tra i due peer. Per questa architettura
HAProxy apre una connessione al backend: non e necessario abilitare IP forwarding
o aggiungere NAT sul VPS. Non applicare ricette iptables generiche che espongano
altre porte o cambino le route preesistenti.

### B2. Prepara il backend e HAProxy

Questo percorso alternativo non usa lo script Tailscale. Prepara `.env` e JSON
seguendo i passi 1-2 della [guida tecnica](FLEET_TELEMETRY.md).
Nel solo [override pubblico](../docker-compose.telemetry-public.yml) configura
il binding WireGuard seguente, senza aggiungere porte nel Compose principale:

```yaml
services:
  fleet-telemetry:
    ports:
      - "10.77.0.2:8443:8443"
```

Mantieni nel `.env` hostname `telemetry.tuodominio.it` e porta pubblica `443`.
Sul firewall domestico consenti TCP `8443` solo dal peer `10.77.0.1` via `wg0`.
La VPN deve essere attiva prima di creare il container con questo binding.
Configura l'ordine di avvio anche dopo un reboot.

Sul VPS installa HAProxy. Su un VPS dedicato puoi usare questa configurazione
minima in `/etc/haproxy/haproxy.cfg`; su un server condiviso integrala senza
sovrascrivere gli altri frontend:

```haproxy
global
    user haproxy
    group haproxy

defaults
    mode tcp
    timeout connect 10s
    timeout client 1h
    timeout server 1h

frontend tesla_telemetry
    bind :443
    default_backend tesla_home

backend tesla_home
    server receiver 10.77.0.2:8443
```

Questa e modalita TCP: nessun `ssl`, `crt`, `send-proxy` o terminazione HTTPS.
I timeout sono esempi di inattivita, da adattare all'esercizio. Non copiare
certificato e chiave TLS sul VPS: la terminazione resta a casa. [12]

Quando il backend e pronto:

```bash
sudo haproxy -c -f /etc/haproxy/haproxy.cfg
sudo systemctl enable --now haproxy
sudo systemctl reload haproxy
```

### B3. Emetti Let's Encrypt con DNS-01 sul server domestico

Installa Certbot e il plugin ufficiale del tuo provider DNS con una modalita
supportata dalla distribuzione, senza mescolare installazioni apt/snap/pip.
DNS-01 dimostra il controllo del dominio mediante un TXT: non richiede porta
`80` pubblica, non richiede IP pubblico domestico e non sostituisce il tunnel TCP
per i dati dell'auto. [13], [14]

Esempio con Cloudflare, valido solo se gestisce la zona DNS e hai installato
`certbot-dns-cloudflare`:

**Passo 1.** Crea un API token limitato alla modifica DNS della sola zona necessaria.

**Passo 2.** Tramite un editor amministrativo crea `/root/.secrets/cloudflare.ini`
con `dns_cloudflare_api_token = IL_TUO_TOKEN`. Proteggi directory con `0700` e
file con `0600`. Non mettere il token in chat, Git o riga di comando.

**Passo 3.** Richiedi il certificato, sostituendo il dominio:

```bash
sudo certbot certonly --dns-cloudflare \
  --dns-cloudflare-credentials /root/.secrets/cloudflare.ini \
  --dns-cloudflare-propagation-seconds 60 \
  --key-type rsa --rsa-key-size 2048 \
  --preferred-chain "ISRG Root X1" \
  --cert-name telemetry.tuodominio.it -d telemetry.tuodominio.it
```

Completa i prompt Certbot per contatto e condizioni del servizio. La propagazione
potrebbe richiedere piu tempo. RSA e X1 sono scelte conservative, non requisiti
Tesla verificati: `--preferred-chain` e una preferenza, non una garanzia della
catena ottenuta. Controlla sempre il certificato emesso. [8], [14]

**Passo 4.** Da `/etc/letsencrypt/live/telemetry.tuodominio.it/` distribuisci
`fullchain.pem` in `config/telemetry/fullchain.pem` e `privkey.pem` in
`config/telemetry/server-key.pem`, con proprietario `TELEMETRY_UID:GID` e
permessi `0600`. Per esempio, **dalla root del progetto**, come utente
proprietario non root:

```bash
TELEMETRY_HOST='telemetry.tuodominio.it'
sudo install -m 600 -o "$(id -u)" -g "$(id -g)" \
  "/etc/letsencrypt/live/$TELEMETRY_HOST/fullchain.pem" \
  "$PWD/config/telemetry/fullchain.pem"
sudo install -m 600 -o "$(id -u)" -g "$(id -g)" \
  "/etc/letsencrypt/live/$TELEMETRY_HOST/privkey.pem" \
  "$PWD/config/telemetry/server-key.pem"
```

La directory del progetto deve essere gia preparata come nella guida tecnica. Non montare
unicamente i symlink `live/` dentro Docker: puntano a `archive/`. Qui si copiano
i file effettivi per mantenere il mount privato gia previsto dal progetto.

**Passo 5.** Prepara una catena CA contenente gli intermedi del tuo certificato
e le radici ufficiali. Il certificato server non deve essere incluso in `ca.pem`.
Per Let's Encrypt, dalla root del progetto con il dominio nella variabile:

```bash
umask 077
curl --fail --location https://letsencrypt.org/certs/isrgrootx1.pem -o config/telemetry/root-x1.pem
curl --fail --location https://letsencrypt.org/certs/isrg-root-x2.pem -o config/telemetry/root-x2.pem
awk '/-----BEGIN CERTIFICATE-----/{certificate++} certificate > 1 {print}' \
  config/telemetry/fullchain.pem > config/telemetry/intermediates.pem
cat config/telemetry/intermediates.pem config/telemetry/root-x1.pem config/telemetry/root-x2.pem \
  > config/telemetry/ca.pem
docker compose -f docker-compose.yml -f docker-compose.telemetry-public.yml \
  run --rm --no-deps --user "$(id -u):$(id -g)" tesla-api ./tesla-api --check-telemetry
docker compose -f docker-compose.yml -f docker-compose.telemetry-public.yml \
  --profile telemetry up -d fleet-telemetry
```

Continua soltanto se la verifica riesce; avvia HAProxy e svolgi A7-A8 usando il
tuo dominio e l'override pubblico anziche quello Tailscale. Su Podman applica
il mapping UID indicato nella guida tecnica anche ai comandi di verifica.

**Passo 6.** Verifica il rinnovo ACME:

```bash
sudo certbot renew --cert-name telemetry.tuodominio.it --dry-run
```

Controlla che il metodo di installazione abbia un timer o cron di rinnovo attivo.
Configura un **deploy hook** che dopo un rinnovo riuscito distribuisca i file,
ricostruisca e verifichi la CA e riavvii il solo ricevitore con l'override pubblico.
Prepara e valida la nuova coppia in staging, conserva un backup privato e ferma
il ricevitore prima di sostituire insieme certificato e chiave. Non usare il
worker Tailscale della sezione 5 per questo dominio. Un cambio CA puo richiedere
una nuova configurazione esplicita dell'auto.
Con Podman rootless il riavvio deve avvenire nel contesto dell'utente corretto,
non in uno stack root separato. Il solo `certbot renew` non aggiorna le copie
nel progetto; `--dry-run` non dimostra che il deploy hook funzioni in produzione.
Il rinnovo manuale DNS senza hook di autenticazione non e automatico. [14]

## 7. Problemi tipici

| Sintomo | Cosa controllare |
| --- | --- |
| VPN funziona ma l'auto non si collega | Ingresso pubblico, DNS e inoltro TCP: una tailnet privata non basta |
| Funnel mostra un endpoint HTTPS ma manca mTLS | Verifica `TCPForward` nella configurazione generata; niente terminazione TLS intermedia |
| Nome certificato non corrispondente | Nome `ts.net` reale o dominio proprio, SAN e `.env` devono coincidere |
| `permission denied` sui PEM | UID/GID, directory `0700`, file `0600`, mapping Podman; niente `777` |
| Porta `443` occupata o vietata in rootless | Nel percorso A non deve esistere una pubblicazione host telemetry; non aggiungere l'override pubblico |
| TLS valido ma browser rifiutato | Puo essere normale: manca il certificato client Tesla |
| Nessun dato dopo un rinnovo | Copie PEM aggiornate, coppia corretta, riavvio, scadenza e CA inviata all'auto |
| `synced: true` ma nessun messaggio | Tunnel, log del ricevitore, errori Tesla, stato auto e cambiamento segnali |
| Backend VPS non raggiungibile | WireGuard attivo, handshake, binding `10.77.0.2`, firewall e ordine di avvio |
| MQTT riceve ma MySQL no | Credenziali, flow Node-RED unico e Deploy; non e un problema del certificato |

Non usare una whitelist basata su un presunto IP Tesla unico: e il veicolo a
collegarsi da reti mobili o Wi-Fi variabili. Mantieni invece mTLS e protezioni
del ricevitore, oltre ai limiti e controlli operativi appropriati.

## 8. Arresto e ripristino

Per interrompere la raccolta rimuovi prima la configurazione dal veicolo,
lasciando telemetry abilitata nell'API fino all'esito della rimozione:

```bash
curl -X DELETE http://localhost:8000/api/vehicle/telemetry/config
curl http://localhost:8000/api/vehicle/telemetry/config
```

Dopo aver verificato l'esito Tesla, nel percorso A ferma ingresso, worker e
ricevitore senza cancellare lo stato:

```bash
docker compose -f docker-compose.yml -f docker-compose.tailscale.yml \
  --profile telemetry stop fleet-telemetry telemetry-certificates tailscale
```

Nel percorso B disabilita solo il frontend HAProxy dedicato e la relativa regola
pubblica, senza arrestare servizi condivisi. Poi puoi impostare telemetry a
`false`. Spegnere VPN, tunnel o container non cancella la configurazione auto.
Non usare `docker compose down -v`: cancellerebbe volumi con dati del progetto.

## 9. Fonti e limiti della verifica

Fonti primarie; la sintassi Tailscale e ancorata al sorgente `v1.104.1`, mentre
il progetto usa Fleet Telemetry `v0.9.5`. Verifica eventuali differenze della
versione realmente installata.

1. [Tesla Fleet Telemetry, README v0.9.5: installazione, mTLS e configurazione](https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/README.md).
2. [Tesla, pagina Fleet Telemetry](https://developer.tesla.com/docs/fleet-api/fleet-telemetry): contenuto non estraibile durante questa verifica; non usato per attribuire una raccomandazione VPN.
3. [Tesla, configurazione TLS e CA client](https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/config/config.go).
4. [Tailscale, sorgente Serve/Funnel e distinzione TCP/TLS terminato](https://github.com/tailscale/tailscale/blob/v1.104.1/cmd/tailscale/cli/serve_v2.go).
5. [Tailscale Funnel](https://tailscale.com/kb/1223/funnel) e [CLI Funnel](https://tailscale.com/kb/1311/tailscale-funnel): pagine da consultare per requisiti, policy e limiti correnti; non estraibili durante questa verifica.
6. [Tailscale HTTPS Certificates](https://tailscale.com/kb/1153/enabling-https) e [implementazione ACME Let's Encrypt](https://github.com/tailscale/tailscale/blob/v1.104.1/feature/acme/cert.go).
7. [Tailscale, comando cert e opzioni di esportazione](https://github.com/tailscale/tailscale/blob/v1.104.1/cmd/tailscale/cli/cert.go).
8. [Let's Encrypt, certificati e catene di fiducia](https://letsencrypt.org/certificates/).
9. [Tesla, validatore del certificato server v0.9.5](https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/tools/check_server_cert.sh).
10. [Tailscale, logica interna di refresh ACME](https://github.com/tailscale/tailscale/blob/v1.104.1/feature/acme/refresh.go).
11. [WireGuard Quick Start](https://www.wireguard.com/quickstart/) e [manuale wg](https://github.com/WireGuard/wireguard-tools/blob/master/src/man/wg.8).
12. [HAProxy, manuale di configurazione e modalita TCP](https://github.com/haproxy/haproxy/blob/v3.2.0/doc/configuration.txt).
13. [Let's Encrypt, challenge DNS-01](https://letsencrypt.org/docs/challenge-types/#dns-01-challenge).
14. [Certbot, uso e rinnovi](https://eff-certbot.readthedocs.io/en/stable/using.html) e [plugin Cloudflare](https://certbot-dns-cloudflare.readthedocs.io/en/stable/).

Non sono stati emessi certificati reali, configurati account Tailscale/DNS/VPS,
o eseguiti test con l'auto. Il pacchetto include il rinnovo nei container, non
timer sull'host. Le prove locali con certificati sintetici coprono rotazione,
rifiuto mTLS e riavvio; non dimostrano il collegamento attraverso Funnel reale.
La verifica end-to-end richiede
endpoint reale, prova esterna, sincronizzazione Tesla e messaggi effettivi nel
database. Per prerequisiti, consenso e salvataggio consulta anche la
[guida semplice](FLEET_TELEMETRY_GUIDA_SEMPLICE.md) e la
[guida tecnica](FLEET_TELEMETRY.md).

[1]: https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/README.md
[2]: https://developer.tesla.com/docs/fleet-api/fleet-telemetry
[3]: https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/config/config.go
[4]: https://github.com/tailscale/tailscale/blob/v1.104.1/cmd/tailscale/cli/serve_v2.go
[5]: https://tailscale.com/kb/1223/funnel
[6]: https://github.com/tailscale/tailscale/blob/v1.104.1/feature/acme/cert.go
[7]: https://github.com/tailscale/tailscale/blob/v1.104.1/cmd/tailscale/cli/cert.go
[8]: https://letsencrypt.org/certificates/
[9]: https://github.com/teslamotors/fleet-telemetry/blob/v0.9.5/tools/check_server_cert.sh
[10]: https://github.com/tailscale/tailscale/blob/v1.104.1/feature/acme/refresh.go
[11]: https://www.wireguard.com/quickstart/
[12]: https://github.com/haproxy/haproxy/blob/v3.2.0/doc/configuration.txt
[13]: https://letsencrypt.org/docs/challenge-types/#dns-01-challenge
[14]: https://eff-certbot.readthedocs.io/en/stable/using.html
