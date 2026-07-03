-- name: GetLatestBlockHeight :one
SELECT COALESCE(MAX(height), 0)::bigint
FROM blocks
WHERE chain_status = 'pending';

-- name: ListActionRows :many
WITH
action_rows AS (
  SELECT
    b.id AS block_id,
    b.height::bigint AS height,
    b.chain_status,
    bzc.sequence_no,
    action_array.action_index,
    zau.id AS account_update_id,
    zc.id AS zkapp_command_id,
    zc.hash,
    fee_pk.value AS fee_payer,
    COALESCE(
      array_agg(field.field ORDER BY action_field.field_index)
        FILTER (WHERE field.field IS NOT NULL),
      ARRAY[]::text[]
    )::text[] AS data
  FROM blocks b
  JOIN blocks_zkapp_commands bzc ON bzc.block_id = b.id
  JOIN zkapp_commands zc ON zc.id = bzc.zkapp_command_id
  JOIN zkapp_fee_payer_body zfpb ON zfpb.id = zc.zkapp_fee_payer_body_id
  JOIN public_keys fee_pk ON fee_pk.id = zfpb.public_key_id
  JOIN LATERAL unnest(zc.zkapp_account_updates_ids)
    WITH ORDINALITY AS command_update(account_update_id, account_update_index) ON true
  JOIN zkapp_account_update zau ON zau.id = command_update.account_update_id
  JOIN zkapp_account_update_body zaub ON zaub.id = zau.body_id
  JOIN account_identifiers ai ON ai.id = zaub.account_identifier_id
  JOIN public_keys account_pk ON account_pk.id = ai.public_key_id
  JOIN zkapp_events actions ON actions.id = zaub.actions_id
  JOIN LATERAL unnest(actions.element_ids)
    WITH ORDINALITY AS action_array(field_array_id, action_index) ON true
  JOIN zkapp_field_array field_array ON field_array.id = action_array.field_array_id
  LEFT JOIN LATERAL unnest(field_array.element_ids)
    WITH ORDINALITY AS action_field(field_id, field_index) ON true
  LEFT JOIN zkapp_field field ON field.id = action_field.field_id
  WHERE account_pk.value = sqlc.arg(contract_address)::text
    AND b.height = sqlc.arg(height)::bigint
  GROUP BY
    b.id,
    b.height,
    b.chain_status,
    bzc.sequence_no,
    action_array.action_index,
    zau.id,
    zc.id,
    zc.hash,
    fee_pk.value
),
deduped_action_rows AS (
  SELECT DISTINCT ON (zkapp_command_id, account_update_id, action_index)
    height,
    fee_payer,
    data
  FROM action_rows
  ORDER BY
    zkapp_command_id,
    account_update_id,
    action_index,
    CASE chain_status
      WHEN 'canonical' THEN 0
      WHEN 'pending' THEN 1
      ELSE 2
    END,
    height DESC,
    block_id DESC,
    sequence_no DESC
)
SELECT height, fee_payer, data
FROM deduped_action_rows
ORDER BY height, fee_payer;
