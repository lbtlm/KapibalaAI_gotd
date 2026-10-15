DROP INDEX pending_message_media;
ALTER TABLE messages DROP COLUMN media_url,DROP COLUMN media_kind,DROP COLUMN media_mime,
 DROP COLUMN media_name,DROP COLUMN media_size,DROP COLUMN media_status,DROP COLUMN media_error,
 DROP COLUMN media_attempts,DROP COLUMN media_retry_at,DROP COLUMN media_downloaded_at,DROP COLUMN local_file_path;
