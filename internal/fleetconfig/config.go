package fleetconfig

import "encoding/json"

func ServerConfig(username, password, certificateFile, keyFile string) ([]byte, error) {
	config := map[string]interface{}{
		"host": "0.0.0.0", "port": 8443, "log_level": "info", "json_log_enable": true,
		"namespace": "tesla", "reliable_ack_sources": map[string]string{"V": "mqtt"},
		"records": map[string][]string{
			"V": {"mqtt"}, "alerts": {"mqtt"}, "errors": {"mqtt"}, "connectivity": {"mqtt"},
		},
		"mqtt": map[string]interface{}{
			"broker": "mosquitto:1883", "client_id": "tesla-fleet-telemetry",
			"username": username, "password": password,
			"topic_base": "tesla", "qos": 1, "retained": false,
		},
		"tls": map[string]string{
			"server_cert": certificateFile,
			"server_key":  keyFile,
		},
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
