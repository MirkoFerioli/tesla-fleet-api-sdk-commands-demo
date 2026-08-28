# Guida completa: avviare proxy Tesla + tesla-cli con Docker Compose

Questa guida avvia **entrambi** i servizi in Docker: il proxy HTTP ufficiale Tesla (`tesla-proxy`, firma i comandi col certificato TLS e la chiave Fleet) e il tuo programma (`tesla-cli`), già collegati tra loro nello stesso `docker-compose.yml`.

## 0. Prerequisiti

- Docker e Docker Compose installati.
- `TESLA_CLIENT_ID`, `TESLA_CLIENT_SECRET` da [developer.tesla.com](https://developer.tesla.com).
- `private-key.pem` (chiave privata Fleet, la controparte della chiave pubblica caricata sul veicolo) nella root del progetto.
- VIN del veicolo.

## 1. Configura le credenziali

```bash
cp .env.example .env
```

Modifica `.env`:

```ini
TESLA_CLIENT_ID=xxx
TESLA_CLIENT_SECRET=xxx
TESLA_REDIRECT_URI=http://localhost:8080/callback
TESLA_VEHICLE_VIN=xxx
```

## 2. Genera certificati TLS e prepara la chiave Fleet

```bash
bash setup-proxy.sh
```

Questo crea:
- `config/tls-cert.pem` e `config/tls-key.pem` (certificato TLS self-signed per il proxy)
- `config/fleet-key.pem` (copia di `private-key.pem`, usata dal proxy per firmare i comandi)

## 3. Crea il file dei token (se non esiste)

Il file deve esistere prima del primo `docker-compose up`, altrimenti Docker crea una cartella al posto del file:

```bash
touch tesla-tokens.json
```

## 4. Build e avvio di entrambi i servizi

Alla primissima esecuzione serve `--build` per costruire l'immagine `tesla-cli`:

```bash
docker-compose up --build -d
```

Cosa succede:
- `tesla-proxy` parte, espone `4443` e usa `config/tls-cert.pem`, `config/tls-key.pem`, `config/fleet-key.pem`.
- `tesla-cli` aspetta che `tesla-proxy` sia **healthy**, poi parte condividendo il network namespace del proxy (`network_mode: service:tesla-proxy`): questo permette al codice Go, che chiama `https://localhost:4443` in modo fisso, di raggiungere il proxy senza modifiche.
- `.env` e `tesla-tokens.json` sono montati come volumi in `/app` dentro `tesla-cli`.

Per gli avvii successivi (nessuna modifica al codice), **non serve** `--build`: Docker Compose riusa l'immagine già costruita.

```bash
docker-compose up -d
```

Usa `--build` di nuovo solo dopo aver modificato [tesla-cli.go](tesla-cli.go) o il [Dockerfile](Dockerfile):

```bash
docker-compose up --build -d
```

## 5. Usa la CLI interattiva

Se hai avviato in background, attacca il terminale al container `tesla-cli`:

```bash
docker attach tesla-cli
```

Vedrai il menu:
```
What would you like to do?
1. Lock Doors
2. Unlock Doors
3. Sentry Mode ON
4. Sentry Mode OFF
5. Quit
```

Al primo avvio, se non trova token validi in `tesla-tokens.json`, il programma apre il flusso OAuth: segui il link stampato in console dal tuo browser (sul host, non nel container), autorizza l'app, e i token verranno salvati in `tesla-tokens.json` grazie al volume condiviso.

Per staccarti dal container senza fermarlo: `Ctrl+P` poi `Ctrl+Q`.

## 6. Controlli utili

```bash
# Stato dei servizi
docker-compose ps

# Log del proxy
docker-compose logs -f tesla-proxy

# Log della CLI
docker-compose logs -f tesla-cli

# Health check manuale del proxy
curl -k https://localhost:4443/health
```

## 7. Fermare tutto

```bash
docker-compose down
```

I file `config/`, `.env`, `tesla-tokens.json` restano sul host: al riavvio non serve rifare il setup (a meno che i token siano scaduti).

## Troubleshooting

| Problema | Causa probabile | Soluzione |
|---|---|---|
| `tesla-cli` non parte, resta in attesa | `tesla-proxy` non risulta "healthy" | `docker-compose logs tesla-proxy`, verifica certificati in `config/` |
| Errore di connessione a `localhost:4443` dentro `tesla-cli` | `network_mode: service:tesla-proxy` mancante o rimosso | Verifica che sia presente in [docker-compose.yml](docker-compose.yml) |
| I token non vengono richiesti/salvati | `tesla-tokens.json` non montato o creato come cartella | Cancella la cartella `tesla-tokens.json` e ricrea il file con `touch tesla-tokens.json` |
| `private-key.pem not found` durante `setup-proxy.sh` | Chiave privata mancante nella root | Copia `private-key.pem` (dal tutorial di generazione chiavi) nella root del progetto |
