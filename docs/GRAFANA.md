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

Per vedere dati, esegui almeno un polling:

```bash
curl -X POST http://localhost:8000/api/vehicle/poll \
  -H "Content-Type: application/json" \
  -d '{"endpoints":"charge_state,drive_state,climate_state,vehicle_state"}'
```

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