# Grafana

Grafana viene avviato dal compose su:

```text
http://localhost:3000
```

Credenziali di default:

- utente: `admin`
- password: `admin`

Puoi cambiarle prima dell'avvio:

```bash
export GRAFANA_ADMIN_USER=admin
export GRAFANA_ADMIN_PASSWORD='scegli-una-password'
docker compose up -d
```

Questi valori inizializzano l'account amministratore al primo avvio. Se `grafana-data` contiene gia un database inizializzato, cambiare le variabili non modifica automaticamente la password esistente: cambiala in Grafana o usa la procedura di reset amministratore, senza cancellare i dati.

## Datasource InfluxDB

Il datasource viene creato automaticamente da [influxdb.yml](../grafana/provisioning/datasources/influxdb.yml):

```text
grafana/provisioning/datasources/influxdb.yml
```

Usa l'URL interno Docker:

```text
http://influxdb:8086
```

Parametri:

- organization: `tesla`
- bucket: `vehicle`
- token: `${INFLUX_TOKEN}` passato dal compose

## Dashboard iniziale

La dashboard provisionata e [tesla-overview.json](../grafana/dashboards/tesla-overview.json):

```text
grafana/dashboards/tesla-overview.json
```

Contiene:

- battery level nel tempo;
- ultimi comandi registrati.

La dashboard si trova nella cartella `Tesla`, usa il datasource `tesla-influxdb` e interroga le ultime 24 ore con refresh ogni 30 secondi. Le query contengono direttamente `range(start: -24h)`: cambiare il selettore temporale di Grafana non modifica quel filtro finche non adatti le query.

Per vedere dati, esegui almeno un polling:

```bash
curl -X POST http://localhost:8000/api/vehicle/poll \
  -H "Content-Type: application/json" \
  -d '{"endpoints":"charge_state,drive_state,climate_state,vehicle_state"}'
```

Il polling alimenta solo il pannello batteria. Per il pannello comandi serve almeno un comando inviato tramite l'API con InfluxDB configurato. La query `last()` restituisce l'ultimo valore per ogni serie e campo, non un elenco globale dei comandi in ordine cronologico. Se non appaiono dati, verifica che la scrittura del poll sia riuscita e che il token del datasource consenta la lettura del bucket `vehicle`.

## Query Flux utili

Battery level:

```flux
from(bucket: "vehicle")
  |> range(start: -24h)
  |> filter(fn: (r) => r._measurement == "tesla_vehicle")
  |> filter(fn: (r) => r._field == "charge_state_battery_level")
```

Ultimi comandi:

```flux
from(bucket: "vehicle")
  |> range(start: -24h)
  |> filter(fn: (r) => r._measurement == "tesla_command")
  |> last()
```