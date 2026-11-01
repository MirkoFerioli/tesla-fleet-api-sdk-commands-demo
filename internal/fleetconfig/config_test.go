package fleetconfig

import (
	"encoding/json"
	"testing"
)

func TestServerConfigPreservesCredentialsAndTLSPaths(t *testing.T) {
	const username, password = "mqtt-user", "quotes\" slash\\ newline\n$"
	data, err := ServerConfig(username, password, "/cert/fullchain.pem", "/cert/server-key.pem")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MQTT struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Broker   string `json:"broker"`
		} `json:"mqtt"`
		TLS struct {
			Certificate string `json:"server_cert"`
			Key         string `json:"server_key"`
		} `json:"tls"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.MQTT.Username != username || config.MQTT.Password != password || config.MQTT.Broker != "mosquitto:1883" {
		t.Fatalf("unexpected MQTT settings: %+v", config.MQTT)
	}
	if config.TLS.Certificate != "/cert/fullchain.pem" || config.TLS.Key != "/cert/server-key.pem" {
		t.Fatalf("unexpected TLS settings: %+v", config.TLS)
	}
}
