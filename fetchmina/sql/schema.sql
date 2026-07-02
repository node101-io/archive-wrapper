CREATE TABLE blocks (
  id BIGINT PRIMARY KEY,
  height BIGINT NOT NULL,
  chain_status TEXT NOT NULL
);

CREATE TABLE blocks_zkapp_commands (
  block_id BIGINT NOT NULL,
  zkapp_command_id BIGINT NOT NULL,
  sequence_no BIGINT NOT NULL
);

CREATE TABLE zkapp_commands (
  id BIGINT PRIMARY KEY,
  zkapp_fee_payer_body_id BIGINT NOT NULL,
  zkapp_account_updates_ids BIGINT[] NOT NULL,
  hash TEXT NOT NULL
);

CREATE TABLE zkapp_fee_payer_body (
  id BIGINT PRIMARY KEY,
  public_key_id BIGINT NOT NULL
);

CREATE TABLE public_keys (
  id BIGINT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE zkapp_account_update (
  id BIGINT PRIMARY KEY,
  body_id BIGINT NOT NULL
);

CREATE TABLE zkapp_account_update_body (
  id BIGINT PRIMARY KEY,
  account_identifier_id BIGINT NOT NULL,
  actions_id BIGINT NOT NULL
);

CREATE TABLE account_identifiers (
  id BIGINT PRIMARY KEY,
  public_key_id BIGINT NOT NULL
);

CREATE TABLE zkapp_events (
  id BIGINT PRIMARY KEY,
  element_ids BIGINT[] NOT NULL
);

CREATE TABLE zkapp_field_array (
  id BIGINT PRIMARY KEY,
  element_ids BIGINT[] NOT NULL
);

CREATE TABLE zkapp_field (
  id BIGINT PRIMARY KEY,
  field TEXT NOT NULL
);