INSERT INTO blocks (id, height, chain_status, global_slot_since_genesis, parent_id, state_hash) VALUES
  (10, 10, 'pending', 10, NULL, 'state-10'),
  (11, 11, 'pending', 11, 10, 'state-11'),
  (12, 12, 'pending', 12, 11, 'state-12'),
  (13, 13, 'pending', 13, 12, 'state-13'),
  (14, 14, 'pending', 14, 13, 'state-14');

INSERT INTO public_keys (id, value) VALUES
  (1, 'B62qjRDirGFRf5dvNcGzMs5oWzQ2VyNcygnoKM2MkxB9PFUp7Utdraf'),
  (2, 'B62qjTpSX2R4fyqJrC9pzvm5PSZb5GMaYBXh8di3TBn6pPC9XXYFC9k');
INSERT INTO zkapp_fee_payer_body (id, public_key_id) VALUES (1, 2);
INSERT INTO account_identifiers (id, public_key_id) VALUES (1, 1);
INSERT INTO zkapp_field (id, field) VALUES
  (1, '1'),
  (2, 'ignored'),
  (3, 'ignored'),
  (4, '42');
INSERT INTO zkapp_field_array (id, element_ids) VALUES (1, ARRAY[1, 2, 3, 4]::bigint[]);
INSERT INTO zkapp_events (id, element_ids) VALUES (1, ARRAY[1]::bigint[]);
INSERT INTO zkapp_account_update_body (id, account_identifier_id, actions_id) VALUES (1, 1, 1);
INSERT INTO zkapp_account_update (id, body_id) VALUES (1, 1);
INSERT INTO zkapp_commands (id, zkapp_fee_payer_body_id, zkapp_account_updates_ids, hash)
VALUES (1, 1, ARRAY[1]::bigint[], 'command-1');
INSERT INTO blocks_zkapp_commands (block_id, zkapp_command_id, sequence_no) VALUES (11, 1, 0);
