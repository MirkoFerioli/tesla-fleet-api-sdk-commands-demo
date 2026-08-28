# Docker: build e push su registry

## 1. Build dell'immagine locale

```bash
docker build -t tesla-fleet-api-sdk-commands-demo:latest .
```

## 2. Test locale

L'app legge `.env` come file fisico nella working dir (non come variabili d'ambiente), quindi va montato come volume:

```bash
docker run --rm -p 8080:8080 \
  -v "$(pwd)/.env:/app/.env:ro" \
  -v "$(pwd)/tesla-tokens.json:/app/tesla-tokens.json" \
  tesla-fleet-api-sdk-commands-demo:latest
```

`tesla-tokens.json` va montato in scrittura (senza `:ro`) perché il programma aggiorna i token dopo il refresh OAuth.

## 3. Tag per il registry

Sostituisci `<registry>` e `<utente>` con i tuoi valori (es. Docker Hub, GHCR, ACR).

```bash
# Docker Hub
docker tag tesla-fleet-api-sdk-commands-demo:latest <utente>/tesla-fleet-api-sdk-commands-demo:latest

# GitHub Container Registry
docker tag tesla-fleet-api-sdk-commands-demo:latest ghcr.io/<utente>/tesla-fleet-api-sdk-commands-demo:latest

# Azure Container Registry
docker tag tesla-fleet-api-sdk-commands-demo:latest <registry>.azurecr.io/tesla-fleet-api-sdk-commands-demo:latest
```

## 4. Login al registry

```bash
# Docker Hub
docker login

# GitHub Container Registry (usa un Personal Access Token con scope write:packages)
docker login ghcr.io -u <utente>

# Azure Container Registry
az acr login --name <registry>
```

## 5. Push dell'immagine

```bash
# Docker Hub
docker push <utente>/tesla-fleet-api-sdk-commands-demo:latest

# GitHub Container Registry
docker push ghcr.io/<utente>/tesla-fleet-api-sdk-commands-demo:latest

# Azure Container Registry
docker push <registry>.azurecr.io/tesla-fleet-api-sdk-commands-demo:latest
```

## 6. Build multi-architettura (opzionale, amd64 + arm64)

```bash
docker buildx create --use
docker buildx build --platform linux/amd64,linux/arm64 \
  -t <utente>/tesla-fleet-api-sdk-commands-demo:latest \
  --push .
```

## Note

- Il file `.dockerignore` esclude `.git`, `.env` e altri file non necessari alla build: verifica che le tue credenziali non finiscano mai nell'immagine.
- Usa un tag di versione (es. `:v1.0.0`) invece di `:latest` per deploy ripetibili.
