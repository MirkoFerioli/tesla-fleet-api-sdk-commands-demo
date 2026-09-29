CREATE TABLE IF NOT EXISTS mqtt_messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  topic VARCHAR(255) NOT NULL,
  payload JSON NOT NULL,
  received_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  INDEX idx_mqtt_messages_received_at (received_at)
);