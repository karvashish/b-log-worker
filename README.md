# Environment Variables

Set the following before running the app:

$env:DATABASE_URL = "postgres://app:apppass@127.0.0.1:5432/app?sslmode=disable"
$env:NATS_URL = "nats://127.0.0.1:4222"
$env:JS_DOMAIN = "prod"

# Running with Air

Use `air` for live reloading during development.

# NATS JetStream

## Subject

`b_log.uploaded`

## Message Structure (uploadEvent)

- original_name (string) : client file name
- stored_name   (string) : renamed file saved on server
- stored_path   (string) : full path on disk
- size          (int64)  : bytes written
- uploaded_at   (time, UTC) : upload timestamp
- user_agent    (string, optional) : HTTP user agent
- remote_addr   (string, optional) : client IP and port

## Notes

- Subscriber binds to JetStream and consumes from subject `b_log.uploaded`.
- Messages are JSON encoded.
- Each message has a unique `Msg-Id` header set to `stored_name`.
