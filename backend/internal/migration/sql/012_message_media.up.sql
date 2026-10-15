ALTER TABLE messages ADD COLUMN media_url text,
 ADD COLUMN media_kind text,
 ADD COLUMN media_mime text,
 ADD COLUMN media_name text,
 ADD COLUMN media_size bigint,
 ADD COLUMN media_status text CHECK(media_status IN ('pending','ready','failed','deleting','expired')),
 ADD COLUMN media_error text,
 ADD COLUMN media_attempts integer NOT NULL DEFAULT 0,
 ADD COLUMN media_retry_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN media_downloaded_at timestamptz,
 ADD COLUMN local_file_path text;
CREATE INDEX pending_message_media ON messages(media_retry_at,id) WHERE media_status='pending';
-- Pre-C1 Telegram updates retained message IDs but discarded media metadata.
UPDATE messages m SET media_url='/media/'||g.gateway_group_id||'_'||m.msg_id,
 media_kind='file',media_status='pending'
FROM groups g WHERE g.id=m.group_id AND g.gateway_group_id ~ '^tg-[0-9]+$'
 AND m.msg_id ~ '^[0-9]+$' AND m.text='' AND m.client_msg_id IS NULL;
